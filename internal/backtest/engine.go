package backtest

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"FinanceSimulator/datasource"
	"FinanceSimulator/matcher"
	"FinanceSimulator/portfolio"
)

// SymbolBar 将一根行情与其所属标的绑定。
type SymbolBar struct {
	Symbol string
	Bar    datasource.Bar
}

// StrategyFunc 根据一根已收盘的行情和本时间点全部成交后的共享账户返回订单。
// 策略可为任意参与回测的标的下单。新订单最早在订单所属标的下一根 bar 成交。
type StrategyFunc func(context.Context, SymbolBar, portfolio.Account) ([]matcher.Order, error)

// EquityPoint 记录一个行情时间点收盘时的组合净值。
type EquityPoint struct {
	Time  time.Time
	Value float64
}

// OrderRejection 记录撮合时因资金或持仓不足而被拒绝的订单。
type OrderRejection struct {
	Order  matcher.Order
	Time   time.Time
	Reason error
}

// Result 包含最终账户、成交、账本和组合净值。
type Result struct {
	Account        portfolio.Account
	Fills          []portfolio.Fill        // 成交记录，包括成交价、数量和时间。
	Ledger         []portfolio.LedgerEntry // 记账明细，包括成交后的现金和已实现盈亏。
	Equity         []EquityPoint
	RejectedOrders []OrderRejection
	UnfilledOrders []matcher.Order // 回测结束时仍在等待所属标的下一根行情的订单。
}

// Run 用同一个账户按时间推进多标的回测，撮合零费用市价单。
// 行情按时间非递减排列，同一标的在同一时间点最多一根 bar，价格采用成交时原价。
// 同时可撮合的订单按提交顺序处理，策略按同一时间点的行情输入顺序调用。
// 初始持仓需要在第一个时间点有收盘价。之后无新行情的持仓沿用上一次收盘价。
// splits 是回测区间内且尚未计入初始账户的拆股事件，Date 表示生效时间。
func Run(ctx context.Context, account portfolio.Account, bars []SymbolBar, onBar StrategyFunc, splits ...datasource.CorporateAction) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if onBar == nil {
		return Result{}, fmt.Errorf("backtest requires a strategy")
	}
	result := Result{Account: account}
	if len(bars) == 0 {
		return result, nil
	}

	var lastFillSeq uint64
	for _, symbol := range account.PositionSymbols() {
		for _, lot := range account.OpenLots(symbol) {
			if lot.OpenTime.After(bars[0].Bar.Time) {
				return Result{}, fmt.Errorf("initial %s position was opened after the first bar", symbol)
			}
			lastFillSeq = max(lastFillSeq, lot.OpenSeq)
		}
	}
	universe := make(map[string]struct{})
	for _, item := range bars {
		universe[item.Symbol] = struct{}{}
	}
	splits = slices.Clone(splits)
	for _, split := range splits {
		if _, ok := universe[split.Symbol]; !ok || split.Date.IsZero() {
			return Result{}, fmt.Errorf("split requires a backtest symbol and effective time: %q at %s", split.Symbol, split.Date)
		}
		if split.Type != datasource.ActionSplit && split.Type != datasource.ActionReverseSplit {
			return Result{}, fmt.Errorf("unsupported backtest corporate action: %q", split.Type)
		}
	}
	slices.SortStableFunc(splits, func(a, b datasource.CorporateAction) int { return a.Date.Compare(b.Date) })
	marks := make(map[string]float64, len(universe))
	openingBars := make(map[string]datasource.Bar, len(universe))
	pending := make(map[string][]matcher.Order, len(universe))
	var ready []matcher.Order
	var nextOrderID uint64

	for start := 0; start < len(bars); {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		timestamp := bars[start].Bar.Time
		if timestamp.IsZero() || (start > 0 && !timestamp.After(bars[start-1].Bar.Time)) {
			return Result{}, fmt.Errorf("bar %d: timestamps must be nonzero and ordered", start+1)
		}

		// 拆股生效后，旧持仓、挂单和缓存价格要先切到新股数口径，再用本轮开盘价撮合。
		for len(splits) > 0 && !splits[0].Date.After(timestamp) {
			if err := applySplit(&result, splits[0], pending, marks); err != nil {
				return Result{}, err
			}
			splits = splits[1:]
		}

		// 先收齐同一时间点的行情。本轮只能撮合此前提交且所属标的已有新 bar 的订单。
		clear(openingBars)
		ready = ready[:0]
		end := start
		for end < len(bars) && bars[end].Bar.Time.Equal(timestamp) {
			item := bars[end]
			if err := validateSymbolBar(item, openingBars); err != nil {
				return Result{}, fmt.Errorf("bar %d: %w", end+1, err)
			}
			openingBars[item.Symbol] = item.Bar
			marks[item.Symbol] = item.Bar.Close
			ready = append(ready, pending[item.Symbol]...)
			delete(pending, item.Symbol)
			end++
		}

		// 多个标的竞争同一笔现金时，先处理较早提交的订单。
		slices.SortFunc(ready, compareOrderIDs)
		for _, order := range ready {
			fill, _, err := matcher.Match(order, openingBars[order.Symbol], matcher.CostModel{})
			if err != nil {
				return Result{}, fmt.Errorf("%s: match order %d: %w", timestamp, order.ID, err)
			}
			if lastFillSeq == math.MaxUint64 {
				return Result{}, fmt.Errorf("%s: fill sequence exhausted", timestamp)
			}
			fill.Seq = lastFillSeq + 1
			next, entries, err := portfolio.ApplyFill(result.Account, *fill)
			if errors.Is(err, portfolio.ErrNegativeCash) || errors.Is(err, portfolio.ErrOversell) {
				result.RejectedOrders = append(result.RejectedOrders, OrderRejection{Order: order, Time: timestamp, Reason: err})
				continue
			}
			if err != nil {
				return Result{}, fmt.Errorf("%s: book order %d: %w", timestamp, order.ID, err)
			}
			lastFillSeq = fill.Seq
			result.Account = next
			result.Fills = append(result.Fills, *fill)
			result.Ledger = append(result.Ledger, entries...)
		}

		// 同一时间点的开盘订单全部处理后，各标的策略调用看到同一份账户状态。
		equity, err := result.Account.Equity(marks)
		if err != nil {
			return Result{}, fmt.Errorf("%s: value account: %w", timestamp, err)
		}
		for index := start; index < end; index++ {
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			orders, err := onBar(ctx, bars[index], result.Account)
			if err != nil {
				return Result{}, fmt.Errorf("bar %d: strategy: %w", index+1, err)
			}
			for _, order := range orders {
				if err := matcher.ValidateOrder(order); err != nil {
					return Result{}, fmt.Errorf("bar %d: %w", index+1, err)
				}
				if _, ok := universe[order.Symbol]; !ok || order.Type != matcher.MarketOrder {
					return Result{}, fmt.Errorf("bar %d: backtest accepts only market orders for symbols with input bars", index+1)
				}
				nextOrderID++
				order.ID = nextOrderID
				pending[order.Symbol] = append(pending[order.Symbol], order)
			}
		}
		result.Equity = append(result.Equity, EquityPoint{Time: timestamp, Value: equity})
		start = end
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	for _, orders := range pending {
		result.UnfilledOrders = append(result.UnfilledOrders, orders...)
	}
	slices.SortFunc(result.UnfilledOrders, compareOrderIDs)
	return result, nil
}

func applySplit(result *Result, split datasource.CorporateAction, pending map[string][]matcher.Order, marks map[string]float64) error {
	next, entries, err := portfolio.ApplyCorporateAction(result.Account, split)
	if err != nil {
		return fmt.Errorf("%s: apply split for %s: %w", split.Date, split.Symbol, err)
	}

	orders := pending[split.Symbol]
	for i := range orders {
		// MVP 只支持市价单，跨拆股的旧挂单只需按比例调整数量。
		orders[i].Qty *= split.SplitRatio
		if err := matcher.ValidateOrder(orders[i]); err != nil {
			return fmt.Errorf("%s: adjust order %d for %s split: %w", split.Date, orders[i].ID, split.Symbol, err)
		}
	}
	if previousClose, ok := marks[split.Symbol]; ok {
		// 拆股日若暂时没有新行情，用换算后的旧收盘价估值。
		mark := previousClose / split.SplitRatio
		if !(mark > 0) || math.IsInf(mark, 0) {
			return fmt.Errorf("%s: invalid post-split mark for %s", split.Date, split.Symbol)
		}
		marks[split.Symbol] = mark
	}
	result.Account = next
	result.Ledger = append(result.Ledger, entries...)
	return nil
}

func validateSymbolBar(item SymbolBar, openingBars map[string]datasource.Bar) error {
	if strings.TrimSpace(item.Symbol) == "" {
		return fmt.Errorf("symbol is required")
	}
	if _, ok := openingBars[item.Symbol]; ok {
		return fmt.Errorf("duplicate bar for %s at %s", item.Symbol, item.Bar.Time)
	}
	if err := matcher.ValidateBar(item.Bar); err != nil {
		return fmt.Errorf("%s: %w", item.Symbol, err)
	}
	if !(item.Bar.Close > 0) || math.IsInf(item.Bar.Close, 0) {
		return fmt.Errorf("%s: close must be positive and finite", item.Symbol)
	}
	return nil
}

func compareOrderIDs(a, b matcher.Order) int {
	return cmp.Compare(a.ID, b.ID)
}
