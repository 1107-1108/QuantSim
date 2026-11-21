package main

import (
	"encoding/json"
	"testing"
	"time"

	"FinanceSimulator/datasource"
)

func TestCollectBacktestData(t *testing.T) {
	var aapl, msft datasource.YahooResponse
	inputs := []struct {
		response *datasource.YahooResponse
		payload  string
	}{
		{&aapl, `{"chart":{"result":[{
			"meta":{"symbol":"AAPL","exchangeTimezoneName":"America/New_York"},
			"timestamp":[1598621400,1598880600,1599053400],
			"indicators":{"quote":[{"open":[25,26,53],"high":[25,26,53],"low":[25,26,53],"close":[25,26,53],"volume":[100,100,100]}]},
			"events":{"splits":{"1598893200":{"date":1598893200,"numerator":4,"denominator":1},"1599053400":{"date":1599053400,"numerator":2,"denominator":1}}}
		}]}}`},
		{&msft, `{"chart":{"result":[{
			"meta":{"symbol":"MSFT"},
			"timestamp":[1598621400,1598880600],
			"indicators":{"quote":[{"open":[200,203],"high":[200,203],"low":[200,203],"close":[200,203],"volume":[100,100]}]}
		}]}}`},
	}
	for _, input := range inputs {
		if err := json.Unmarshal([]byte(input.payload), input.response); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Date(2020, 8, 28, 0, 0, 0, 0, time.UTC)
	end := time.Date(2020, 9, 2, 0, 0, 0, 0, time.UTC)
	bars, splits, err := collectBacktestData([]datasource.YahooResponse{aapl, msft}, start, end)
	if err != nil {
		t.Fatal(err)
	}
	// 回测区间外的拆股参与价格还原，但不会作为账户事件交给引擎。
	if len(splits) != 1 || splits[0].Symbol != "AAPL" || splits[0].SplitRatio != 4 ||
		!splits[0].Date.Equal(time.Date(2020, 8, 31, 4, 0, 0, 0, time.UTC)) {
		t.Errorf("unexpected replayed splits: %+v", splits)
	}
	wantSymbols := []string{"AAPL", "MSFT", "AAPL", "MSFT"}
	wantCloses := []float64{200, 200, 52, 203}
	if len(bars) != len(wantSymbols) {
		t.Fatalf("got %d bars, want %d", len(bars), len(wantSymbols))
	}
	for i, item := range bars {
		if item.Symbol != wantSymbols[i] || item.Bar.Close != wantCloses[i] || item.Bar.PriceBasis != datasource.PriceAsTraded {
			t.Errorf("bar %d = %+v, want %s as-traded close %g", i, item, wantSymbols[i], wantCloses[i])
		}
	}
}
