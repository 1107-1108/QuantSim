package portfolio

import (
	"fmt"
	"math"
	"time"
)

// Side 买卖方向。方向单独表达,数量恒为正——
// 这样"Qty > 0"才能成为一条可校验的硬规则,而不是靠正负号猜方向。
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// Fill 一笔成交记录:matcher 撮合的产出、ApplyFill 的输入。
// portfolio 不感知订单(订单是 matcher 的概念),只认成交。
type Fill struct {
	Seq        uint64 // 全局递增成交序号:FIFO 顺序来源,也是账本 join 键
	OrderID    uint64 // 来源订单(Go 侧统一分配,事件循环对账用)
	Symbol     string
	Side       Side
	Qty        float64   // 成交数量,恒 > 0
	Price      float64   // 实际成交价:原始价口径、滑点已含,恒 > 0
	Commission float64   // 本笔总佣金(不是每股),恒 ≥ 0
	Time       time.Time // 成交时间(回测=撮合 bar 的时间)
}

// validate 校验成交记录,任何字段非法 → ErrInvalidFill。
// matcher 产出的 Fill 本应合法,所以这里失败 = 上游 bug,fail fast。
func (f Fill) validate() error {
	switch {
	case f.Symbol == "":
		return fmt.Errorf("%w: empty symbol", ErrInvalidFill)
	case f.Side != SideBuy && f.Side != SideSell:
		return fmt.Errorf("%w: invalid side %q", ErrInvalidFill, f.Side)
	case math.IsNaN(f.Qty) || f.Qty <= 0:
		return fmt.Errorf("%w: qty must be positive: %v", ErrInvalidFill, f.Qty)
	case math.IsNaN(f.Price) || f.Price <= 0:
		return fmt.Errorf("%w: price must be positive: %v", ErrInvalidFill, f.Price)
	case math.IsNaN(f.Commission) || f.Commission < 0:
		return fmt.Errorf("%w: commission must not be negative: %v", ErrInvalidFill, f.Commission)
	}
	return nil
}

// ApplyFill 把一笔成交记到账上——portfolio 包的核心入口。
//
// 不可变约定:传入的 account 不被改动;返回新 Account + 本次新增的账本条目。
// 出错时返回(原 account, nil, err),不产生任何状态变更。
//
// 买入:总成本 = 数量×价 + 佣金 → 佣金摊入每股成本 → 队尾追加 lot → 扣现金。
// 卖出:FIFO 从队首逐 lot 消耗 → 净收入按消耗比例分摊 → 每个 lot 记一条已实现。
func ApplyFill(account Account, fill Fill) (Account, []LedgerEntry, error) {
	if err := fill.validate(); err != nil {
		return account, nil, err
	}
	if fill.Side == SideBuy {
		return applyBuy(account, fill)
	}
	return applySell(account, fill)
}

// applyBuy 买入分支。
//
// 为什么佣金摊入每股成本:买入花的每一分钱(股价+佣金)都是持仓成本,
// 摊入成本后,卖出时的 realized = 卖出净收入 − 成本,天然就是"含费口径"的盈亏。
func applyBuy(account Account, fill Fill) (Account, []LedgerEntry, error) {
	// 1. 总成本 = 股价 + 佣金;每股成本 = 总成本 ÷ 数量
	totalCost := fill.Qty*fill.Price + fill.Commission
	costPerShare := totalCost / fill.Qty

	// 2. 不可变性:先 clone,之后所有改动只作用在副本上
	next := account.clone()

	// 3. 队尾追加新 lot——FIFO 的对称性:买入永远追加队尾,卖出永远消耗队首,
	//    队列天然保持 (建仓时间, 成交序号) 升序
	symbol := InstrumentID(fill.Symbol)
	next.positions[symbol] = append(next.positions[symbol], Lot{
		openTime:     fill.Time,
		openSeq:      fill.Seq,
		RemainQty:    fill.Qty,
		costPerShare: costPerShare,
	})

	// 4. 扣现金。现金为负 = 上游购买力校验漏了的 bug:
	//    返回错误并丢弃副本(不产生新状态)
	next.cash -= totalCost
	if next.cash < 0 {
		return account, nil, fmt.Errorf("%w: buy %s costs %v, cash only %v",
			ErrNegativeCash, fill.Symbol, totalCost, account.cash)
	}

	// 5. 记账:一笔买入一条 TRADE;CashAfter = 事件后现金
	entry := LedgerEntry{
		Time:       fill.Time,
		Type:       EntryTrade,
		Symbol:     fill.Symbol,
		Qty:        fill.Qty,
		Price:      fill.Price,
		Commission: fill.Commission,
		CashAfter:  next.cash,
		FillSeq:    fill.Seq,
	}
	return next, []LedgerEntry{entry}, nil
}

// applySell 卖出分支(FIFO 消耗)。
//
// 规则:从队首(最早建仓)的 lot 开始消耗;卖出净收入按各 lot 的
// 消耗数量占比分摊;每个被消耗的 lot 单独记一条 REALIZED_PNL,
// 这样每笔盈亏都能回溯到具体哪笔买入(LotOpenSeq)。
func applySell(account Account, fill Fill) (Account, []LedgerEntry, error) {
	symbol := InstrumentID(fill.Symbol)

	// 1. 持仓校验:超卖 = 上游持仓校验漏了的 bug
	if held := account.PositionQty(fill.Symbol); fill.Qty > held {
		return account, nil, fmt.Errorf("%w: sell %s %v shares, position only %v",
			ErrOversell, fill.Symbol, fill.Qty, held)
	}

	// 2. 卖出净收入 = 成交额 − 佣金(卖出佣金从收入扣,与买入佣金摊入成本对称)
	netProceeds := fill.Qty*fill.Price - fill.Commission

	next := account.clone()
	queue := next.positions[symbol]

	var entries []LedgerEntry
	remaining := fill.Qty   // 还需消耗的数量
	allocatedSum := 0.0     // 已分摊出去的净收入(供末笔吸收浮点残差)

	for remaining > 0 {
		lot := queue[0]
		take := math.Min(remaining, lot.RemainQty) // 本 lot 被消耗的数量

		// 本 lot 分摊的净收入。
		// take == remaining 时 take 就是 remaining 本身(浮点完全相等,可安全比较),
		// 说明这是最后一笔消耗:直接拿"总净收入 − 已分摊",把前面逐笔的
		// 浮点误差全部吸收掉,保证 Σ分摊 == netProceeds 精确闭合。
		var alloc float64
		if take == remaining {
			alloc = netProceeds - allocatedSum
		} else {
			alloc = netProceeds * take / fill.Qty
			allocatedSum += alloc
		}

		// 已实现盈亏 = 分摊净收入 − 消耗数量 × 该 lot 的每股成本
		realized := alloc - take*lot.costPerShare
		entries = append(entries, LedgerEntry{
			Time:        fill.Time,
			Type:        EntryRealizedPnL,
			Symbol:      fill.Symbol,
			Qty:         take,
			Price:       fill.Price,
			RealizedPnL: realized,
			FillSeq:     fill.Seq,
			LotOpenSeq:  lot.openSeq,
		})

		remaining -= take
		if take == lot.RemainQty {
			queue = queue[1:] // 全部消耗:出队
		} else {
			// 部分消耗:剩余部分留在原位(FIFO 顺序不变),
			// 数量减少,其余字段(成本/建仓时间/序号)不动
			lot.RemainQty -= take
			queue[0] = lot
		}
	}

	// 3. 清仓的 symbol 不留空队列
	if len(queue) == 0 {
		delete(next.positions, symbol)
	} else {
		next.positions[symbol] = queue
	}

	// 4. 加现金(卖出是进钱,不可能变负,无需校验)
	next.cash += netProceeds

	// 5. 记账:同一事件的所有条目 CashAfter 统一填事件后现金;
	//    REALIZED 在前,一条合计的 TRADE 收尾
	for i := range entries {
		entries[i].CashAfter = next.cash
	}
	entries = append(entries, LedgerEntry{
		Time:       fill.Time,
		Type:       EntryTrade,
		Symbol:     fill.Symbol,
		Qty:        fill.Qty,
		Price:      fill.Price,
		Commission: fill.Commission,
		CashAfter:  next.cash,
		FillSeq:    fill.Seq,
	})
	return next, entries, nil
}
