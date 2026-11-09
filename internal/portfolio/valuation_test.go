package portfolio

import (
	"errors"
	"math"
	"testing"
	"time"
)

// 构造一个带持仓的账户供估值测试使用
func newAccountWithPositions(cash float64, positions map[string][]Lot) Account {
	return Account{
		initialCash: cash,
		cash:        cash,
		positions:   positions,
	}
}

// TestMarketValueUsesHeldQuantity 验证市值仅基于当前持仓数量，无持仓时为零。
func TestMarketValueUsesHeldQuantity(t *testing.T) {
	account := newTwoPositionAccount()

	if got := account.MarketValue("AAPL", 15); got != 3_000 {
		t.Errorf("MarketValue(AAPL) = %v, want 3000", got)
	}
	if got := account.MarketValue("NVDA", 900); got != 0 {
		t.Errorf("MarketValue(NVDA) = %v, want 0", got)
	}
}

// TestValuationsRejectNaNMarks 防止无效市价泄漏到净值、盈亏或前端账户投影。
func TestValuationsRejectNaNMarks(t *testing.T) {
	account := newTwoPositionAccount()
	marks := map[string]float64{"AAPL": math.NaN(), "MSFT": 50}

	if _, err := account.Equity(marks); !errors.Is(err, ErrMissingMark) {
		t.Errorf("Equity error = %v, want ErrMissingMark", err)
	}
	if _, err := account.UnrealizedPnL(marks); !errors.Is(err, ErrMissingMark) {
		t.Errorf("UnrealizedPnL error = %v, want ErrMissingMark", err)
	}
	if view, err := account.AccountView(marks); !errors.Is(err, ErrMissingMark) || view.Cash != 0 || view.Equity != 0 || len(view.Positions) != 0 {
		t.Errorf("AccountView = %+v, %v; want empty view and ErrMissingMark", view, err)
	}
}

func TestEquityReturnsCashWhenNoPositions(t *testing.T) {
	// Arrange
	account := newAccountWithPositions(10_000, map[string][]Lot{})

	// Act
	equity, err := account.Equity(map[string]float64{})

	// Assert
	if err != nil {
		t.Fatalf("Equity returned unexpected error: %v", err)
	}
	if equity != 10_000 {
		t.Errorf("equity = %v, want %v", equity, 10_000)
	}
}

func TestEquitySumsPositionsAtMark(t *testing.T) {
	// Arrange:两个标的、其中一个含多个 lot;成本价与 Equity 无关,故意取离谱值
	account := newAccountWithPositions(500, map[string][]Lot{
		"AAPL": {
			{OpenTime: time.Now(), OpenSeq: 1, Qty: 100, CostPerShare: 1},
			{OpenTime: time.Now(), OpenSeq: 2, Qty: 50, CostPerShare: 2},
		},
		"MSFT": {
			{OpenTime: time.Now(), OpenSeq: 3, Qty: 10, CostPerShare: 3},
		},
	})
	marks := map[string]float64{"AAPL": 30, "MSFT": 200}

	// Act
	equity, err := account.Equity(marks)

	// Assert:500 + 150×30 + 10×200 = 7000
	if err != nil {
		t.Fatalf("Equity returned unexpected error: %v", err)
	}
	if equity != 7_000 {
		t.Errorf("equity = %v, want %v", equity, 7_000)
	}
}

func TestEquityReturnsErrMissingMarkWhenPositionLacksMark(t *testing.T) {
	// Arrange
	account := newAccountWithPositions(500, map[string][]Lot{
		"AAPL": {{OpenTime: time.Now(), OpenSeq: 1, Qty: 100, CostPerShare: 50}},
	})

	// Act
	_, err := account.Equity(map[string]float64{})

	// Assert
	if !errors.Is(err, ErrMissingMark) {
		t.Errorf("err = %v, want ErrMissingMark", err)
	}
}

func TestEquityIgnoresMarksForSymbolsNotHeld(t *testing.T) {
	// Arrange
	account := newAccountWithPositions(1_000, map[string][]Lot{})
	marks := map[string]float64{"AAPL": 30, "TSLA": 200}

	// Act
	equity, err := account.Equity(marks)

	// Assert
	if err != nil {
		t.Fatalf("Equity returned unexpected error: %v", err)
	}
	if equity != 1_000 {
		t.Errorf("equity = %v, want %v", equity, 1_000)
	}
}

func TestEquitySkipsZeroQuantityPositions(t *testing.T) {
	// Arrange:队列里只剩零数量的 lot,不视为持仓,不要求 mark
	account := newAccountWithPositions(1_000, map[string][]Lot{
		"AAPL": {{OpenTime: time.Now(), OpenSeq: 1, Qty: 0, CostPerShare: 50}},
	})

	// Act
	equity, err := account.Equity(map[string]float64{})

	// Assert
	if err != nil {
		t.Fatalf("Equity returned unexpected error: %v", err)
	}
	if equity != 1_000 {
		t.Errorf("equity = %v, want %v", equity, 1_000)
	}
}

// 精确可整除的测试数据:AAPL 200 股成本 2000(avg 10)、MSFT 10 股成本 1000(avg 100)、现金 1000
func newTwoPositionAccount() Account {
	return newAccountWithPositions(1_000, map[string][]Lot{
		"AAPL": {
			{OpenTime: time.Now(), OpenSeq: 1, Qty: 100, CostPerShare: 8},
			{OpenTime: time.Now(), OpenSeq: 2, Qty: 100, CostPerShare: 12},
		},
		"MSFT": {
			{OpenTime: time.Now(), OpenSeq: 3, Qty: 10, CostPerShare: 100},
		},
	})
}

func twoPositionMarks() map[string]float64 {
	return map[string]float64{"AAPL": 15, "MSFT": 50}
}

func TestUnrealizedPnLSumsAcrossPositions(t *testing.T) {
	// Arrange
	account := newTwoPositionAccount()

	// Act
	pnl, err := account.UnrealizedPnL(twoPositionMarks())

	// Assert:(200×15 − 2000) + (10×50 − 1000) = 1000 + (−500) = 500
	if err != nil {
		t.Fatalf("UnrealizedPnL returned unexpected error: %v", err)
	}
	if pnl != 500 {
		t.Errorf("pnl = %v, want %v", pnl, 500.0)
	}
}

func TestUnrealizedPnLReturnsErrMissingMarkWhenPositionLacksMark(t *testing.T) {
	// Arrange
	account := newTwoPositionAccount()

	// Act:只给 AAPL 的 mark,MSFT 缺
	_, err := account.UnrealizedPnL(map[string]float64{"AAPL": 15})

	// Assert
	if !errors.Is(err, ErrMissingMark) {
		t.Errorf("err = %v, want ErrMissingMark", err)
	}
}

func TestUnrealizedPnLReturnsZeroWhenNoPositions(t *testing.T) {
	// Arrange
	account := newAccountWithPositions(1_000, map[string][]Lot{})

	// Act
	pnl, err := account.UnrealizedPnL(map[string]float64{})

	// Assert
	if err != nil {
		t.Fatalf("UnrealizedPnL returned unexpected error: %v", err)
	}
	if pnl != 0 {
		t.Errorf("pnl = %v, want 0", pnl)
	}
}

func TestAccountViewAggregatesAndSortsPositions(t *testing.T) {
	// Arrange
	account := newTwoPositionAccount()

	// Act
	view, err := account.AccountView(twoPositionMarks())

	// Assert:账户级汇总
	if err != nil {
		t.Fatalf("AccountView returned unexpected error: %v", err)
	}
	if view.Cash != 1_000 {
		t.Errorf("Cash = %v, want 1000", view.Cash)
	}
	if view.Equity != 4_500 { // 1000 + 3000 + 500
		t.Errorf("Equity = %v, want 4500", view.Equity)
	}
	if view.CostBasis != 3_000 { // 2000 + 1000
		t.Errorf("CostBasis = %v, want 3000", view.CostBasis)
	}
	if view.UnrealizedPnL != 500 {
		t.Errorf("UnrealizedPnL = %v, want 500", view.UnrealizedPnL)
	}

	// Assert:持仓按 symbol 字典序,逐字段核对
	if len(view.Positions) != 2 {
		t.Fatalf("len(Positions) = %d, want 2", len(view.Positions))
	}
	wantAAPL := PositionView{Symbol: "AAPL", Qty: 200, AvgCost: 10, Mark: 15, MarketValue: 3000, UnrealizedPnL: 1000}
	if view.Positions[0] != wantAAPL {
		t.Errorf("Positions[0] = %+v, want %+v", view.Positions[0], wantAAPL)
	}
	wantMSFT := PositionView{Symbol: "MSFT", Qty: 10, AvgCost: 100, Mark: 50, MarketValue: 500, UnrealizedPnL: -500}
	if view.Positions[1] != wantMSFT {
		t.Errorf("Positions[1] = %+v, want %+v", view.Positions[1], wantMSFT)
	}
}

func TestAccountViewReturnsErrMissingMarkWhenPositionLacksMark(t *testing.T) {
	// Arrange
	account := newTwoPositionAccount()

	// Act
	_, err := account.AccountView(map[string]float64{})

	// Assert
	if !errors.Is(err, ErrMissingMark) {
		t.Errorf("err = %v, want ErrMissingMark", err)
	}
}

func TestAccountViewWithNoPositions(t *testing.T) {
	// Arrange
	account := newAccountWithPositions(1_000, map[string][]Lot{})

	// Act
	view, err := account.AccountView(map[string]float64{})

	// Assert
	if err != nil {
		t.Fatalf("AccountView returned unexpected error: %v", err)
	}
	if view.Cash != 1_000 || view.Equity != 1_000 {
		t.Errorf("Cash/Equity = %v/%v, want 1000/1000", view.Cash, view.Equity)
	}
	if len(view.Positions) != 0 {
		t.Errorf("len(Positions) = %d, want 0", len(view.Positions))
	}
}
