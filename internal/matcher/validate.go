package matcher

import (
	"fmt"
	"math"

	"FinanceSimulator/datasource"
	"FinanceSimulator/portfolio"
)

// validateOrder 订单边界校验。matcher 的订单来自不可信的上游(Python 策略),
// 任何字段非法都 fail fast——放行会产生一笔把账记坏的 Fill。
func validateOrder(order Order) error {
	switch {
	case order.Symbol == "":
		return fmt.Errorf("%w: empty symbol", ErrInvalidOrder)
	case order.Side != portfolio.SideBuy && order.Side != portfolio.SideSell:
		return fmt.Errorf("%w: invalid side %q", ErrInvalidOrder, order.Side)
	case order.Type != MarketOrder && order.Type != LimitOrder:
		return fmt.Errorf("%w: unknown order type %q", ErrInvalidOrder, order.Type)
	case math.IsNaN(order.Qty) || math.IsInf(order.Qty, 0) || order.Qty <= 0:
		return fmt.Errorf("%w: qty must be positive: %v", ErrInvalidOrder, order.Qty)
	case order.Type == LimitOrder && !isPositiveFinite(order.LimitPrice):
		return fmt.Errorf("%w: limit price must be positive: %v", ErrInvalidOrder, order.LimitPrice)
	}
	return nil
}

// validateBar 行情边界校验。只查撮合实际用到的 OHL 三个价
// (OHLC 一致性、缺口等数据质量规则是 datasource 校验器的职责,不在此重复)。
func validateBar(bar datasource.Bar) error {
	if !isPositiveFinite(bar.Open) || !isPositiveFinite(bar.High) || !isPositiveFinite(bar.Low) {
		return fmt.Errorf("%w: OHL prices must be positive and finite, got O=%v H=%v L=%v",
			ErrInvalidBar, bar.Open, bar.High, bar.Low)
	}
	return nil
}

func isPositiveFinite(x float64) bool {
	return !math.IsNaN(x) && !math.IsInf(x, 0) && x > 0
}
