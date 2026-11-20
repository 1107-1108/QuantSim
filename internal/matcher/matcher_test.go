package matcher_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"FinanceSimulator/datasource"
	"FinanceSimulator/matcher"
	"FinanceSimulator/portfolio"
)

// 全局数值约定:等值比较用容差 1e-6(DESIGN-DETAIL 第 0 节)
func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= 1e-6
}

// testBar 是固定的撮合环境:开盘 100 / 最高 102 / 最低 99 / 收盘 101
var testBar = datasource.Bar{
	Time:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	Open:   100,
	High:   102,
	Low:    99,
	Close:  101,
	Volume: 1_000_000,
}

func TestMatch_MarketBuyFillsAtOpen(t *testing.T) {
	// Arrange:市价买单 + 零成本模型(= 不含费报告口径)
	order := matcher.Order{
		ID:     1,
		Symbol: "AAPL",
		Qty:    10,
		Type:   matcher.MarketOrder,
		Side:   portfolio.SideBuy,
	}

	// Act
	fill, status, err := matcher.Match(order, testBar, matcher.CostModel{})

	// Assert
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if status != matcher.MatchFilled {
		t.Fatalf("status = %v, want FILLED", status)
	}
	if fill == nil {
		t.Fatal("fill is nil, want a fill")
	}
	if !almostEqual(fill.Price, testBar.Open) {
		t.Errorf("fill.Price = %v, want bar open %v", fill.Price, testBar.Open)
	}
	if !almostEqual(fill.Qty, 10) {
		t.Errorf("fill.Qty = %v, want 10", fill.Qty)
	}
	if !almostEqual(fill.Commission, 0) {
		t.Errorf("fill.Commission = %v, want 0 under zero cost model", fill.Commission)
	}
	if !fill.Time.Equal(testBar.Time) {
		t.Errorf("fill.Time = %v, want bar time %v", fill.Time, testBar.Time)
	}
	if fill.OrderID != 1 {
		t.Errorf("fill.OrderID = %v, want 1", fill.OrderID)
	}
	if fill.Symbol != "AAPL" || fill.Side != portfolio.SideBuy {
		t.Errorf("fill Symbol/Side = %v/%v, want AAPL/BUY", fill.Symbol, fill.Side)
	}
}

func TestMatch_LimitBuyFillsAtOpenWhenOpenBelowLimit(t *testing.T) {
	// Arrange:限价 101,开盘价 100 低于限价 → 按更优的开盘价成交
	order := matcher.Order{
		ID:         2,
		Symbol:     "AAPL",
		Qty:        10,
		Type:       matcher.LimitOrder,
		Side:       portfolio.SideBuy,
		LimitPrice: 101,
	}

	// Act
	fill, status, err := matcher.Match(order, testBar, matcher.CostModel{})

	// Assert
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if status != matcher.MatchFilled {
		t.Fatalf("status = %v, want FILLED", status)
	}
	if !almostEqual(fill.Price, testBar.Open) {
		t.Errorf("fill.Price = %v, want open %v (better than limit)", fill.Price, testBar.Open)
	}
}

func TestMatch_LimitBuyFillsAtLimitWhenOpenAboveLimit(t *testing.T) {
	// Arrange:限价 99.5,开盘价 100 比限价差,但 bar 最低 99 触及限价 → 按限价成交
	order := matcher.Order{
		ID:         3,
		Symbol:     "AAPL",
		Qty:        10,
		Type:       matcher.LimitOrder,
		Side:       portfolio.SideBuy,
		LimitPrice: 99.5,
	}

	// Act
	fill, status, err := matcher.Match(order, testBar, matcher.CostModel{})

	// Assert
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if status != matcher.MatchFilled {
		t.Fatalf("status = %v, want FILLED", status)
	}
	if !almostEqual(fill.Price, 99.5) {
		t.Errorf("fill.Price = %v, want limit 99.5", fill.Price)
	}
}

func TestMatch_LimitBuyNoFillWhenLowAboveLimit(t *testing.T) {
	// Arrange:限价 98,bar 最低 99 始终高于限价 → 全天没人愿意这个价卖给你
	order := matcher.Order{
		ID:         4,
		Symbol:     "AAPL",
		Qty:        10,
		Type:       matcher.LimitOrder,
		Side:       portfolio.SideBuy,
		LimitPrice: 98,
	}

	// Act
	fill, status, err := matcher.Match(order, testBar, matcher.CostModel{})

	// Assert
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if status != matcher.MatchNoFill {
		t.Errorf("status = %v, want NO_FILL", status)
	}
	if fill != nil {
		t.Errorf("fill = %+v, want nil when not filled", fill)
	}
}

func TestMatch_LimitSellFillsAtLimitWhenOpenBelowLimit(t *testing.T) {
	// Arrange:限价卖 101,开盘价 100 比限价差,bar 最高 102 触及 → 按限价 101 成交
	order := matcher.Order{
		ID:         5,
		Symbol:     "AAPL",
		Qty:        10,
		Type:       matcher.LimitOrder,
		Side:       portfolio.SideSell,
		LimitPrice: 101,
	}

	// Act
	fill, status, err := matcher.Match(order, testBar, matcher.CostModel{})

	// Assert
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if status != matcher.MatchFilled {
		t.Fatalf("status = %v, want FILLED", status)
	}
	if !almostEqual(fill.Price, 101) {
		t.Errorf("fill.Price = %v, want limit 101", fill.Price)
	}
}

func TestMatch_LimitSellFillsAtOpenWhenOpenAboveLimit(t *testing.T) {
	// Arrange:限价卖 99,开盘价 100 高于限价 → 按更优的开盘价成交
	order := matcher.Order{
		ID:         6,
		Symbol:     "AAPL",
		Qty:        10,
		Type:       matcher.LimitOrder,
		Side:       portfolio.SideSell,
		LimitPrice: 99,
	}

	// Act
	fill, status, err := matcher.Match(order, testBar, matcher.CostModel{})

	// Assert
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if status != matcher.MatchFilled {
		t.Fatalf("status = %v, want FILLED", status)
	}
	if !almostEqual(fill.Price, testBar.Open) {
		t.Errorf("fill.Price = %v, want open %v (better than limit)", fill.Price, testBar.Open)
	}
}

func TestMatch_LimitSellNoFillWhenHighBelowLimit(t *testing.T) {
	// Arrange:限价卖 103,bar 最高 102 始终低于限价 → 不成交
	order := matcher.Order{
		ID:         7,
		Symbol:     "AAPL",
		Qty:        10,
		Type:       matcher.LimitOrder,
		Side:       portfolio.SideSell,
		LimitPrice: 103,
	}

	// Act
	fill, status, err := matcher.Match(order, testBar, matcher.CostModel{})

	// Assert
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if status != matcher.MatchNoFill {
		t.Errorf("status = %v, want NO_FILL", status)
	}
	if fill != nil {
		t.Errorf("fill = %+v, want nil when not filled", fill)
	}
}

func TestMatch_LimitPriceBoundsSlippage(t *testing.T) {
	cases := []struct {
		name       string
		side       portfolio.Side
		limitPrice float64
		wantPrice  float64
	}{
		{name: "buy touched intrabar", side: portfolio.SideBuy, limitPrice: 99.5, wantPrice: 99.5},
		{name: "sell touched intrabar", side: portfolio.SideSell, limitPrice: 101, wantPrice: 101},
		{name: "buy slippage crosses limit from open", side: portfolio.SideBuy, limitPrice: 100.05, wantPrice: 100.05},
		{name: "sell slippage crosses limit from open", side: portfolio.SideSell, limitPrice: 99.95, wantPrice: 99.95},
		{name: "buy slippage stays within limit", side: portfolio.SideBuy, limitPrice: 101, wantPrice: 100.1},
		{name: "sell slippage stays within limit", side: portfolio.SideSell, limitPrice: 99, wantPrice: 99.9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			order := matcher.Order{ID: 1, Symbol: "AAPL", Qty: 10, Type: matcher.LimitOrder, Side: tc.side, LimitPrice: tc.limitPrice}

			fill, status, err := matcher.Match(order, testBar, matcher.CostModel{SlippageBps: 10})
			if err != nil || status != matcher.MatchFilled || fill == nil {
				t.Fatalf("Match returned fill %+v, status %v and error %v, want a fill", fill, status, err)
			}
			if !almostEqual(fill.Price, tc.wantPrice) {
				t.Errorf("fill price = %v, want %v", fill.Price, tc.wantPrice)
			}
		})
	}
}

func TestMatch_SlippageRaisesBuyPrice(t *testing.T) {
	// Arrange:10bps 滑点,买入价朝不利方向(上)偏移:100 × 1.001 = 100.1
	order := matcher.Order{
		ID:     8,
		Symbol: "AAPL",
		Qty:    10,
		Type:   matcher.MarketOrder,
		Side:   portfolio.SideBuy,
	}
	cost := matcher.CostModel{SlippageBps: 10}

	// Act
	fill, status, err := matcher.Match(order, testBar, cost)

	// Assert
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if status != matcher.MatchFilled {
		t.Fatalf("status = %v, want FILLED", status)
	}
	if want := 100 * 1.001; !almostEqual(fill.Price, want) {
		t.Errorf("fill.Price = %v, want %v (open + 10bps slippage)", fill.Price, want)
	}
}

func TestMatch_SlippageLowersSellPrice(t *testing.T) {
	// Arrange:卖出滑点方向相反(朝下):100 × 0.999 = 99.9
	order := matcher.Order{
		ID:     9,
		Symbol: "AAPL",
		Qty:    10,
		Type:   matcher.MarketOrder,
		Side:   portfolio.SideSell,
	}
	cost := matcher.CostModel{SlippageBps: 10}

	// Act
	fill, _, err := matcher.Match(order, testBar, cost)

	// Assert
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if want := 100 * 0.999; !almostEqual(fill.Price, want) {
		t.Errorf("fill.Price = %v, want %v (open - 10bps slippage)", fill.Price, want)
	}
}

func TestMatch_CommissionPerShareAboveMinimum(t *testing.T) {
	// Arrange:200 股 × $0.01 = $2.00,高于 $1.00 最低档 → 按每股佣金收
	order := matcher.Order{
		ID:     10,
		Symbol: "AAPL",
		Qty:    200,
		Type:   matcher.MarketOrder,
		Side:   portfolio.SideBuy,
	}
	cost := matcher.CostModel{CommissionPerShare: 0.01, MinPerOrder: 1.0}

	// Act
	fill, _, err := matcher.Match(order, testBar, cost)

	// Assert
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if !almostEqual(fill.Commission, 2.0) {
		t.Errorf("fill.Commission = %v, want 2.0 (200 × 0.01)", fill.Commission)
	}
}

func TestMatch_CommissionFloorsAtMinimum(t *testing.T) {
	// Arrange:50 股 × $0.01 = $0.50,低于 $1.00 最低档 → 收最低档
	order := matcher.Order{
		ID:     11,
		Symbol: "AAPL",
		Qty:    50,
		Type:   matcher.MarketOrder,
		Side:   portfolio.SideBuy,
	}
	cost := matcher.CostModel{CommissionPerShare: 0.01, MinPerOrder: 1.0}

	// Act
	fill, _, err := matcher.Match(order, testBar, cost)

	// Assert
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if !almostEqual(fill.Commission, 1.0) {
		t.Errorf("fill.Commission = %v, want 1.0 (min per order)", fill.Commission)
	}
}

func TestMatch_RejectsInvalidOrders(t *testing.T) {
	cases := []struct {
		name  string
		order matcher.Order
	}{
		{"empty symbol", matcher.Order{ID: 1, Symbol: "", Qty: 10, Type: matcher.MarketOrder, Side: portfolio.SideBuy}},
		{"zero qty", matcher.Order{ID: 1, Symbol: "AAPL", Qty: 0, Type: matcher.MarketOrder, Side: portfolio.SideBuy}},
		{"negative qty", matcher.Order{ID: 1, Symbol: "AAPL", Qty: -5, Type: matcher.MarketOrder, Side: portfolio.SideBuy}},
		{"NaN qty", matcher.Order{ID: 1, Symbol: "AAPL", Qty: math.NaN(), Type: matcher.MarketOrder, Side: portfolio.SideBuy}},
		{"unknown order type", matcher.Order{ID: 1, Symbol: "AAPL", Qty: 10, Type: "ICEBERG", Side: portfolio.SideBuy}},
		{"unknown side", matcher.Order{ID: 1, Symbol: "AAPL", Qty: 10, Type: matcher.MarketOrder, Side: "HOLD"}},
		{"limit order without price", matcher.Order{ID: 1, Symbol: "AAPL", Qty: 10, Type: matcher.LimitOrder, Side: portfolio.SideBuy, LimitPrice: 0}},
		{"limit order with NaN price", matcher.Order{ID: 1, Symbol: "AAPL", Qty: 10, Type: matcher.LimitOrder, Side: portfolio.SideBuy, LimitPrice: math.NaN()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			fill, _, err := matcher.Match(tc.order, testBar, matcher.CostModel{})

			// Assert:非法订单 = 上游 bug,fail fast,不产生成交
			if !errors.Is(err, matcher.ErrInvalidOrder) {
				t.Errorf("err = %v, want ErrInvalidOrder", err)
			}
			if fill != nil {
				t.Errorf("fill = %+v, want nil on invalid order", fill)
			}
		})
	}
}

func TestMatch_RejectsInvalidBar(t *testing.T) {
	order := matcher.Order{ID: 1, Symbol: "AAPL", Qty: 10, Type: matcher.MarketOrder, Side: portfolio.SideBuy}
	cases := []struct {
		name   string
		mutate func(bar *datasource.Bar)
	}{
		{"NaN open", func(bar *datasource.Bar) { bar.Open = math.NaN() }},
		{"zero open", func(bar *datasource.Bar) { bar.Open = 0 }},
		{"negative high", func(bar *datasource.Bar) { bar.High = -1 }},
		{"NaN low", func(bar *datasource.Bar) { bar.Low = math.NaN() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange:行情数据损坏 ≠ 不成交,必须报错(数据管道漏检的 bug)
			bar := testBar
			tc.mutate(&bar)

			// Act
			_, _, err := matcher.Match(order, bar, matcher.CostModel{})

			// Assert
			if !errors.Is(err, matcher.ErrInvalidBar) {
				t.Errorf("err = %v, want ErrInvalidBar", err)
			}
		})
	}
}
