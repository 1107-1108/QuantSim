package matcher

import (
	"math"

	"FinanceSimulator/datasource"
	"FinanceSimulator/portfolio"
)

type MatchStatus string

const (
	MatchFilled MatchStatus = "FILLED"
	MatchNoFill MatchStatus = "NO_FILL"
)

// CostModel 交易摩擦模型:滑点 + 佣金。
// 整体置零 = 不含费口径,用于"含费/不含费两版报告"的对照组。
type CostModel struct {
	CommissionPerShare float64 // 每股佣金
	MinPerOrder        float64 // 单笔最低佣金
	SlippageBps        float64 // 滑点,单位基点(1bp = 0.01%),默认 10bps
}

// Match 撮合一笔订单:无状态纯函数,回测与 paper trading 共用。
//
// 输入:订单(意图)+ 一根 bar(市场的现实)+ 成本模型(交易的摩擦)。
// 输出:成交则返回 Fill(status = FILLED);未成交返回 (nil, NO_FILL, nil);
// 输入非法返回错误(= 上游 bug,fail fast,此时 status 无意义)。
//
// matcher 不知道 T/T+1:订单该在哪根 bar 撮合是事件循环的选择。
// Fill.Seq 不由本函数赋值:成交序号由事件循环全局递增分配,保证回放确定性。
func Match(order Order, bar datasource.Bar, cost CostModel) (*portfolio.Fill, MatchStatus, error) {
	if err := validateOrder(order); err != nil {
		return nil, "", err
	}
	if err := validateBar(bar); err != nil {
		return nil, "", err
	}
	price, ok := basePrice(order, bar)
	if !ok {
		return nil, MatchNoFill, nil
	}
	price = applySlippage(price, order.Side, cost.SlippageBps)
	// 触及限价即触发成交。最终成交价受买单上限和卖单下限约束。
	if order.Type == LimitOrder {
		if order.Side == portfolio.SideBuy {
			price = math.Min(price, order.LimitPrice)
		} else {
			price = math.Max(price, order.LimitPrice)
		}
	}
	return &portfolio.Fill{
		OrderID:    order.ID,
		Symbol:     order.Symbol,
		Side:       order.Side,
		Qty:        order.Qty,
		Price:      price,
		Commission: commission(order.Qty, cost),
		Time:       bar.Time,
	}, MatchFilled, nil
}

// commission 券商收费模型:每股佣金 × 股数,但有单笔最低消费。
// 零成本模型下 max(0, 0) = 0,天然兼容不含费口径。
func commission(qty float64, cost CostModel) float64 {
	return math.Max(qty*cost.CommissionPerShare, cost.MinPerOrder)
}

// basePrice 成交规则表(滑点前的基准价):
//
//	市价单        → 开盘价成交
//	限价买单 L    → bar 最低价触及(low ≤ L)才成交,按 min(open, L)
//	限价卖单 L    → bar 最高价触及(high ≥ L)才成交,按 max(open, L)
//
// 限价是"最坏可接受价":市场给了更优的开盘价,就按开盘价成交。
// 未触及返回 ok = false(DAY 单当天未成交即作废,由事件循环丢弃)。
func basePrice(order Order, bar datasource.Bar) (price float64, ok bool) {
	switch order.Type {
	case MarketOrder:
		return bar.Open, true
	case LimitOrder:
		if order.Side == portfolio.SideBuy && bar.Low <= order.LimitPrice {
			return math.Min(bar.Open, order.LimitPrice), true
		}
		if order.Side == portfolio.SideSell && bar.High >= order.LimitPrice {
			return math.Max(bar.Open, order.LimitPrice), true
		}
	}
	return 0, false
}

// applySlippage 滑点永远朝不利方向偏移:买入更贵,卖出更便宜。
// 保守建模是故意的:宁可回测少赚,不要收益虚高。
func applySlippage(price float64, side portfolio.Side, bps float64) float64 {
	factor := bps / 10_000 // 基点 → 小数:1bp = 0.01% = 0.0001
	if side == portfolio.SideBuy {
		return price * (1 + factor)
	}
	return price * (1 - factor)
}
