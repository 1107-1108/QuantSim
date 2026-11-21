package backtest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"FinanceSimulator/backtest"
	"FinanceSimulator/datasource"
	"FinanceSimulator/matcher"
	"FinanceSimulator/portfolio"
)

func TestRunFillsOnNextBarBeforeCallingStrategy(t *testing.T) {
	// 第一根收盘价是 100，第二根跳空到 110 开盘，用来验证实际成交用的是哪一个价格。
	first := time.Date(2024, 4, 2, 14, 30, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	bars := []backtest.SymbolBar{
		quote("AAPL", first, 99, 100),
		quote("AAPL", second, 110, 112),
	}
	buy := matcher.Order{Symbol: "AAPL", Side: portfolio.SideBuy, Type: matcher.MarketOrder, Qty: 1}
	var seen []time.Time
	// 测试策略每根都买一股。第二次调用时，第一笔订单应该已经成交并扣款。
	onBar := func(_ context.Context, bar backtest.SymbolBar, account portfolio.Account) ([]matcher.Order, error) {
		seen = append(seen, bar.Bar.Time)
		wantCash, wantQty := 1000.0, 0.0
		if bar.Bar.Time.Equal(second) {
			wantCash, wantQty = 890, 1
		}
		if account.Cash() != wantCash || account.PositionQty("AAPL") != wantQty {
			t.Errorf("strategy at %v saw cash=%v, qty=%v, want %v, %v",
				bar.Bar.Time, account.Cash(), account.PositionQty("AAPL"), wantCash, wantQty)
		}
		return []matcher.Order{buy}, nil
	}

	result, err := backtest.Run(t.Context(), initialAccount(t, 1000), bars, onBar)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || !seen[0].Equal(first) || !seen[1].Equal(second) {
		t.Fatalf("unexpected strategy calls: %v", seen)
	}
	if len(result.Fills) != 1 {
		t.Fatalf("fills = %+v, want one fill", result.Fills)
	}
	fill := result.Fills[0]
	if fill.Price != 110 || !fill.Time.Equal(second) || fill.OrderID != 1 || fill.Seq != 1 {
		t.Errorf("fill = %+v, want the first order filled at the second open (110)", fill)
	}
	if len(result.Ledger) != 1 || result.Ledger[0].FillSeq != fill.Seq || result.Ledger[0].CashAfter != 890 {
		t.Errorf("unexpected ledger: %+v", result.Ledger)
	}
	// 第一根没有持仓，净值是 1000。第二根收盘按 112 估值，净值是 890 + 112。
	if len(result.Equity) != 2 || !result.Equity[0].Time.Equal(first) || result.Equity[0].Value != 1000 ||
		!result.Equity[1].Time.Equal(second) || result.Equity[1].Value != 1002 {
		t.Errorf("equity = %+v, want 1000 and 1002 at their respective closes", result.Equity)
	}
	// 第二笔买单要等第三根才能撮合，所以回测结束时仍未成交。
	if len(result.UnfilledOrders) != 1 || result.UnfilledOrders[0].ID != 2 {
		t.Errorf("unfilled orders = %+v, want the second order", result.UnfilledOrders)
	}
}

func TestRunRejectsDuplicateSymbolBarInSameBatch(t *testing.T) {
	bar := quote("AAPL", time.Date(2024, 4, 2, 14, 30, 0, 0, time.UTC), 100, 100)
	calls := 0
	onBar := func(context.Context, backtest.SymbolBar, portfolio.Account) ([]matcher.Order, error) {
		calls++
		return nil, nil
	}

	_, err := backtest.Run(t.Context(), initialAccount(t, 1000), []backtest.SymbolBar{bar, bar}, onBar)
	if err == nil || calls != 0 {
		t.Fatalf("got err=%v and %d strategy calls, want rejection before processing the batch", err, calls)
	}
}

func TestRunSharesCashAcrossSymbols(t *testing.T) {
	first := time.Date(2024, 4, 2, 14, 30, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	bars := []backtest.SymbolBar{
		quote("AAPL", first, 69, 70), quote("MSFT", first, 59, 60),
		// 交换行情顺序，验证资金竞争仍按订单提交顺序处理。
		quote("MSFT", second, 60, 63), quote("AAPL", second, 70, 75),
	}
	account := initialAccount(t, 100)
	onBar := func(_ context.Context, bar backtest.SymbolBar, snapshot portfolio.Account) ([]matcher.Order, error) {
		if bar.Bar.Time.Equal(first) {
			if snapshot.Cash() != 100 {
				t.Errorf("first batch cash = %v, want 100", snapshot.Cash())
			}
			return []matcher.Order{marketBuy(bar.Symbol)}, nil
		}
		if snapshot.Cash() != 30 || snapshot.PositionQty("AAPL") != 1 || snapshot.PositionQty("MSFT") != 0 {
			t.Errorf("%s saw the wrong shared account after matching at the open", bar.Symbol)
		}
		return nil, nil
	}

	result, err := backtest.Run(t.Context(), account, bars, onBar)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Fills) != 1 || result.Fills[0].Symbol != "AAPL" || result.Fills[0].Price != 70 {
		t.Errorf("unexpected fills: %+v", result.Fills)
	}
	if len(result.RejectedOrders) != 1 || result.RejectedOrders[0].Order.Symbol != "MSFT" ||
		!errors.Is(result.RejectedOrders[0].Reason, portfolio.ErrNegativeCash) {
		t.Errorf("unexpected rejected orders: %+v", result.RejectedOrders)
	}
	if len(result.Equity) != 2 || result.Equity[0].Value != 100 || result.Equity[1].Value != 105 {
		t.Errorf("unexpected equity curve: %+v", result.Equity)
	}
	if account.Cash() != 100 || account.PositionQty("AAPL") != 0 {
		t.Error("the input account was modified")
	}
}

func TestRunWaitsForOrderSymbolAndKeepsLastKnownMarks(t *testing.T) {
	first := time.Date(2024, 4, 2, 14, 30, 0, 0, time.UTC)
	second, third := first.Add(time.Minute), first.Add(2*time.Minute)
	account, _, err := portfolio.ApplyFill(initialAccount(t, 1000), portfolio.Fill{
		Seq: 7, Symbol: "AAPL", Side: portfolio.SideBuy, Qty: 1, Price: 100, Time: first.Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	bars := []backtest.SymbolBar{
		quote("AAPL", first, 109, 110), quote("MSFT", first, 49, 50),
		quote("AAPL", second, 112, 115), quote("MSFT", third, 60, 65),
	}
	onBar := func(_ context.Context, bar backtest.SymbolBar, snapshot portfolio.Account) ([]matcher.Order, error) {
		if bar.Symbol == "AAPL" && bar.Bar.Time.Equal(first) {
			return []matcher.Order{marketBuy("MSFT")}, nil
		}
		if bar.Bar.Time.Before(third) && snapshot.PositionQty("MSFT") != 0 {
			t.Error("MSFT filled before its next bar")
		}
		if bar.Bar.Time.Equal(third) {
			return []matcher.Order{marketBuy("AAPL")}, nil
		}
		return nil, nil
	}

	result, err := backtest.Run(t.Context(), account, bars, onBar)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Fills) != 1 || result.Fills[0].Symbol != "MSFT" || result.Fills[0].Price != 60 ||
		!result.Fills[0].Time.Equal(third) || result.Fills[0].Seq != 8 {
		t.Errorf("unexpected fills: %+v", result.Fills)
	}
	if len(result.Equity) != 3 || result.Equity[0].Value != 1010 || result.Equity[1].Value != 1015 || result.Equity[2].Value != 1020 {
		t.Errorf("unexpected equity curve: %+v", result.Equity)
	}
	if len(result.UnfilledOrders) != 1 || result.UnfilledOrders[0].Symbol != "AAPL" || result.UnfilledOrders[0].ID != 2 {
		t.Errorf("unexpected unfilled orders: %+v", result.UnfilledOrders)
	}
}

func TestRunRequiresCurrentMarkForInitialHoldings(t *testing.T) {
	first := time.Date(2024, 4, 2, 14, 30, 0, 0, time.UTC)
	account, _, err := portfolio.ApplyFill(initialAccount(t, 1000), portfolio.Fill{
		Seq: 1, Symbol: "MSFT", Side: portfolio.SideBuy, Qty: 1, Price: 200, Time: first.Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	bars := []backtest.SymbolBar{
		quote("AAPL", first, 100, 100),
		quote("MSFT", first.Add(time.Minute), 210, 210),
	}
	called := false
	onBar := func(context.Context, backtest.SymbolBar, portfolio.Account) ([]matcher.Order, error) {
		called = true
		return nil, nil
	}
	if _, err := backtest.Run(t.Context(), account, bars, onBar); !errors.Is(err, portfolio.ErrMissingMark) || called {
		t.Fatalf("got %v and called=%v, want a missing mark error before the first strategy call", err, called)
	}
}

func TestRunAppliesSplitBeforeStrategyAndResizesPendingOrder(t *testing.T) {
	friday := time.Date(2020, 8, 28, 13, 30, 0, 0, time.UTC)
	monday := friday.AddDate(0, 0, 3)
	tuesday := monday.AddDate(0, 0, 1)
	account, _, err := portfolio.ApplyFill(initialAccount(t, 1000), portfolio.Fill{
		Seq: 7, Symbol: "AAPL", Side: portfolio.SideBuy, Qty: 1, Price: 400, Time: friday.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	bars := []backtest.SymbolBar{
		quote("AAPL", friday, 400, 400),
		quote("MSFT", monday, 200, 200), // 拆股日 AAPL 缺一根行情，净值仍要使用同口径的估值。
		quote("AAPL", tuesday, 100, 100),
	}
	split := datasource.CorporateAction{
		Symbol: "AAPL", Type: datasource.ActionSplit, Date: monday.Add(-time.Hour), SplitRatio: 4,
	}
	onBar := func(_ context.Context, item backtest.SymbolBar, snapshot portfolio.Account) ([]matcher.Order, error) {
		if item.Bar.Time.Equal(friday) {
			return []matcher.Order{{Symbol: "AAPL", Type: matcher.MarketOrder, Side: portfolio.SideSell, Qty: 1}}, nil
		}
		if item.Bar.Time.Equal(monday) && (snapshot.Cash() != 600 || snapshot.PositionQty("AAPL") != 4 || snapshot.AvgCost("AAPL") != 100) {
			t.Errorf("split-date strategy saw cash=%g, shares=%g and cost=%g", snapshot.Cash(), snapshot.PositionQty("AAPL"), snapshot.AvgCost("AAPL"))
		}
		return nil, nil
	}

	result, err := backtest.Run(t.Context(), account, bars, onBar, split)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Fills) != 1 || result.Fills[0].Qty != 4 || result.Fills[0].Price != 100 || !result.Fills[0].Time.Equal(tuesday) ||
		result.Account.PositionQty("AAPL") != 0 || result.Account.Cash() != 1000 {
		t.Errorf("unexpected fills or final account: %+v, cash=%g", result.Fills, result.Account.Cash())
	}
	if len(result.Ledger) != 3 || result.Ledger[0].Type != portfolio.EntrySplitAdjustment || result.Ledger[0].Qty != 4 {
		t.Errorf("expected one split adjustment before the sale, got %+v", result.Ledger)
	}
	if len(result.Equity) != 3 {
		t.Fatalf("want three equity points, got %+v", result.Equity)
	}
	for _, point := range result.Equity {
		if point.Value != 1000 {
			t.Errorf("equity fell across the split: %+v", result.Equity)
			break
		}
	}
}

func initialAccount(t *testing.T, cash float64) portfolio.Account {
	t.Helper()
	account, err := portfolio.NewAccount(cash)
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func quote(symbol string, timestamp time.Time, open, close float64) backtest.SymbolBar {
	return backtest.SymbolBar{Symbol: symbol, Bar: datasource.Bar{
		Time: timestamp, Open: open, High: max(open, close), Low: min(open, close), Close: close,
	}}
}

func marketBuy(symbol string) matcher.Order {
	return matcher.Order{Symbol: symbol, Side: portfolio.SideBuy, Type: matcher.MarketOrder, Qty: 1}
}
