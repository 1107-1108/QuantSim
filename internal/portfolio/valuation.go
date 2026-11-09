package portfolio

import (
	"fmt"
	"math"
	"slices"
)

// Equity 账户净值 = 现金 + Σ(持仓数量 × 市价)
// 持仓标的缺 mark → ErrMissingMark;数量为 0 的持仓跳过,不要求 mark
func (account *Account) Equity(marks map[string]float64) (float64, error) {
	total := account.cash
	for symbol := range account.positions {
		qty := account.PositionQty(symbol)
		if qty == 0 {
			continue
		}
		mark, err := markForPosition(marks, symbol)
		if err != nil {
			return 0, err
		}
		total += qty * mark
	}
	return total, nil
}

// MarketValue 返回某个标的在给定市价下的市值；无持仓时返回零。
func (account *Account) MarketValue(symbol string, mark float64) float64 {
	return account.PositionQty(symbol) * mark
}

// UnrealizedPnL 未实现盈亏/浮动盈亏:Σ(市值 − 成本基础)
// 持仓标的缺 mark → ErrMissingMark;数量为 0 的持仓跳过,不要求 mark
func (account *Account) UnrealizedPnL(marks map[string]float64) (float64, error) {
	var total float64
	for symbol := range account.positions {
		qty := account.PositionQty(symbol)
		if qty == 0 {
			continue
		}
		mark, err := markForPosition(marks, symbol)
		if err != nil {
			return 0, err
		}
		total += qty*mark - account.CostBasis(symbol)
	}
	return total, nil
}

// PositionView 单个标的的持仓投影
type PositionView struct {
	Symbol        string
	Qty           float64
	AvgCost       float64
	Mark          float64
	MarketValue   float64
	UnrealizedPnL float64
}

// AccountView 账户视图:给前端的一页快照;持仓按 symbol 排序,输出确定
type AccountView struct {
	Cash          float64
	Equity        float64
	CostBasis     float64
	UnrealizedPnL float64
	Positions     []PositionView
}

// AccountView 汇总账户级与各持仓的展示数据
// 持仓标的缺 mark → ErrMissingMark;数量为 0 的持仓跳过
func (account *Account) AccountView(marks map[string]float64) (AccountView, error) {
	view := AccountView{Cash: account.cash, Equity: account.cash}

	symbols := make([]string, 0, len(account.positions))
	for symbol := range account.positions {
		symbols = append(symbols, symbol)
	}
	slices.Sort(symbols)

	for _, symbol := range symbols {
		qty := account.PositionQty(symbol)
		if qty == 0 {
			continue
		}
		mark, err := markForPosition(marks, symbol)
		if err != nil {
			return AccountView{}, err
		}
		costBasis := account.CostBasis(symbol)
		marketValue := qty * mark
		unrealizedPnL := marketValue - costBasis

		view.Positions = append(view.Positions, PositionView{
			Symbol:        symbol,
			Qty:           qty,
			AvgCost:       account.AvgCost(symbol),
			Mark:          mark,
			MarketValue:   marketValue,
			UnrealizedPnL: unrealizedPnL,
		})
		view.CostBasis += costBasis
		view.UnrealizedPnL += unrealizedPnL
		view.Equity += marketValue
	}
	return view, nil
}

// markForPosition 返回一个可用于估值的市价；缺失或非有限数值都不能参与净值计算。
func markForPosition(marks map[string]float64, symbol string) (float64, error) {
	mark, ok := marks[symbol]
	if !ok || math.IsNaN(mark) || math.IsInf(mark, 0) {
		return 0, fmt.Errorf("%w: %s", ErrMissingMark, symbol)
	}
	return mark, nil
}
