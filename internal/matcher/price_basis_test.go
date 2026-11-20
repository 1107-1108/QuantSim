package matcher_test

import (
	"errors"
	"testing"

	"FinanceSimulator/datasource"
	"FinanceSimulator/matcher"
	"FinanceSimulator/portfolio"
)

func TestMatchRequiresAsTradedPrices(t *testing.T) {
	order := matcher.Order{Symbol: "AAPL", Side: portfolio.SideBuy, Type: matcher.MarketOrder, Qty: 1}
	bar := testBar
	for _, basis := range []datasource.PriceBasis{datasource.PriceSplitAdjusted, datasource.PriceBasisUnknown} {
		bar.PriceBasis = basis
		if fill, _, err := matcher.Match(order, bar, matcher.CostModel{}); fill != nil || !errors.Is(err, matcher.ErrInvalidBar) {
			t.Fatalf("got fill %v, error %v, want ErrInvalidBar for basis %v", fill, err, basis)
		}
	}
	bar.PriceBasis = datasource.PriceSplitAdjusted
	converted, err := bar.AsTraded(4)
	if err != nil {
		t.Fatal(err)
	}
	fill, status, err := matcher.Match(order, converted, matcher.CostModel{})
	if err != nil || status != matcher.MatchFilled || fill == nil || fill.Price != 400 {
		t.Fatalf("got %v, %v, %v, want a fill at 400", fill, status, err)
	}
}
