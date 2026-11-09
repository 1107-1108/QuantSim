package portfolio

import (
	"math"
	"testing"
	"time"
)

// TestNewAccountRejectsInvalidInitialCash 防止非法初始现金进入账户状态机。
func TestNewAccountRejectsInvalidInitialCash(t *testing.T) {
	for _, initialCash := range []float64{-1, math.NaN(), math.Inf(1)} {
		if _, err := NewAccount(initialCash); err == nil {
			t.Errorf("NewAccount(%v) error = nil, want error", initialCash)
		}
	}
}

// TestNewAccountInitializesReadableState 验证零现金账户仍是可用的空状态。
func TestNewAccountInitializesReadableState(t *testing.T) {
	account, err := NewAccount(0)
	if err != nil {
		t.Fatalf("NewAccount(0): %v", err)
	}
	if account.Cash() != 0 || account.InitialCash() != 0 || account.BuyingPower() != 0 {
		t.Errorf("zero-cash account queries = cash %v, initial %v, buying power %v, want all zero",
			account.Cash(), account.InitialCash(), account.BuyingPower())
	}
}

// TestAccountQueriesAggregateLots 验证查询从多个 FIFO 批次汇总而非依赖单一批次。
func TestAccountQueriesAggregateLots(t *testing.T) {
	account := Account{
		initialCash: 1_000,
		cash:        700,
		positions: map[string][]Lot{
			"AAPL": {
				{OpenTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), OpenSeq: 1, Qty: 10, CostPerShare: 20},
				{OpenTime: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), OpenSeq: 2, Qty: 5, CostPerShare: 40},
			},
		},
	}

	if got := account.PositionQty("AAPL"); got != 15 {
		t.Errorf("PositionQty(AAPL) = %v, want 15", got)
	}
	if got := account.CostBasis("AAPL"); got != 400 {
		t.Errorf("CostBasis(AAPL) = %v, want 400", got)
	}
	if got := account.AvgCost("AAPL"); got != 400.0/15.0 {
		t.Errorf("AvgCost(AAPL) = %v, want %v", got, 400.0/15.0)
	}
	if got := account.AvgCost("MSFT"); got != 0 {
		t.Errorf("AvgCost(MSFT) = %v, want 0 for no position", got)
	}
}
