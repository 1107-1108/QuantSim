package matcher

import "FinanceSimulator/portfolio"

type OrderType string

const (
	MarketOrder OrderType = "MARKET"
	LimitOrder  OrderType = "LIMIT"
)

// Order 一笔交易意图:策略或手动下单产生,由事件循环在"下一根 bar"交给 Match 撮合(T+1)。
// 没有时间字段:成交时间取自撮合 bar 的时间,订单本身的创建时间撮合不需要。
// TimeInForce 只有 DAY(MVP):当天未成交即作废,故不显式建模。
type Order struct {
	ID         uint64 // Go 侧统一分配,Fill.OrderID 回指它,事件循环对账用
	Symbol     string
	Qty        float64 // 恒 > 0(方向由 Side 表达,数量不带符号)
	Type       OrderType
	Side       portfolio.Side
	LimitPrice float64 // 仅限价单使用;市价单忽略
}
