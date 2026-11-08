package portfolio

import (
	"errors"
	"testing"
	"time"
)

// 构造一个带持仓的账户供估值测试使用
func newAccountWithPositions(cash float64, positions map[InstrumentID][]Lot) Account {
	return Account{
		initialCash: cash,
		cash:        cash,
		positions:   positions,
	}
}

func TestEquityReturnsCashWhenNoPositions(t *testing.T) {
	// Arrange
	account := newAccountWithPositions(10_000, map[InstrumentID][]Lot{})

	// Act
	equity, err := account.Equity(map[InstrumentID]float64{})

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
	account := newAccountWithPositions(500, map[InstrumentID][]Lot{
		"AAPL": {
			{openTime: time.Now(), openSeq: 1, RemainQty: 100, costPerShare: 1},
			{openTime: time.Now(), openSeq: 2, RemainQty: 50, costPerShare: 2},
		},
		"MSFT": {
			{openTime: time.Now(), openSeq: 3, RemainQty: 10, costPerShare: 3},
		},
	})
	marks := map[InstrumentID]float64{"AAPL": 30, "MSFT": 200}

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
	account := newAccountWithPositions(500, map[InstrumentID][]Lot{
		"AAPL": {{openTime: time.Now(), openSeq: 1, RemainQty: 100, costPerShare: 50}},
	})

	// Act
	_, err := account.Equity(map[InstrumentID]float64{})

	// Assert
	if !errors.Is(err, ErrMissingMark) {
		t.Errorf("err = %v, want ErrMissingMark", err)
	}
}

func TestEquityIgnoresMarksForSymbolsNotHeld(t *testing.T) {
	// Arrange
	account := newAccountWithPositions(1_000, map[InstrumentID][]Lot{})
	marks := map[InstrumentID]float64{"AAPL": 30, "TSLA": 200}

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
	account := newAccountWithPositions(1_000, map[InstrumentID][]Lot{
		"AAPL": {{openTime: time.Now(), openSeq: 1, RemainQty: 0, costPerShare: 50}},
	})

	// Act
	equity, err := account.Equity(map[InstrumentID]float64{})

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
	return newAccountWithPositions(1_000, map[InstrumentID][]Lot{
		"AAPL": {
			{openTime: time.Now(), openSeq: 1, RemainQty: 100, costPerShare: 8},
			{openTime: time.Now(), openSeq: 2, RemainQty: 100, costPerShare: 12},
		},
		"MSFT": {
			{openTime: time.Now(), openSeq: 3, RemainQty: 10, costPerShare: 100},
		},
	})
}

func twoPositionMarks() map[InstrumentID]float64 {
	return map[InstrumentID]float64{"AAPL": 15, "MSFT": 50}
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
	_, err := account.UnrealizedPnL(map[InstrumentID]float64{"AAPL": 15})

	// Assert
	if !errors.Is(err, ErrMissingMark) {
		t.Errorf("err = %v, want ErrMissingMark", err)
	}
}

func TestUnrealizedPnLReturnsZeroWhenNoPositions(t *testing.T) {
	// Arrange
	account := newAccountWithPositions(1_000, map[InstrumentID][]Lot{})

	// Act
	pnl, err := account.UnrealizedPnL(map[InstrumentID]float64{})

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
	_, err := account.AccountView(map[InstrumentID]float64{})

	// Assert
	if !errors.Is(err, ErrMissingMark) {
		t.Errorf("err = %v, want ErrMissingMark", err)
	}
}

func TestAccountViewWithNoPositions(t *testing.T) {
	// Arrange
	account := newAccountWithPositions(1_000, map[InstrumentID][]Lot{})

	// Act
	view, err := account.AccountView(map[InstrumentID]float64{})

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
