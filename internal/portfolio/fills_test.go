package portfolio

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"
)

// 测试用固定时间,避免 time.Now() 带来的不可比对性
var (
	testTime1 = time.Date(2024, 1, 2, 9, 30, 0, 0, time.UTC)
	testTime2 = time.Date(2024, 1, 3, 9, 30, 0, 0, time.UTC)
)

// snapshotAccount 深拷贝账户快照,用于验证 ApplyFill 不改动传入的原账户
func snapshotAccount(a Account) Account {
	c := Account{
		initialCash: a.initialCash,
		cash:        a.cash,
		positions:   make(map[InstrumentID][]Lot, len(a.positions)),
	}
	for symbol, lots := range a.positions {
		c.positions[symbol] = slices.Clone(lots)
	}
	return c
}

// almostEqual 浮点比较:容许 1e-9 量级的计算误差
func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b))
}

func TestApplyFillRejectsInvalidFills(t *testing.T) {
	valid := Fill{Seq: 1, Symbol: "AAPL", Side: SideBuy, Qty: 100, Price: 100, Commission: 1, Time: testTime1}
	cases := []struct {
		name   string
		mutate func(*Fill)
	}{
		{"空 symbol", func(f *Fill) { f.Symbol = "" }},
		{"非法买卖方向", func(f *Fill) { f.Side = "HOLD" }},
		{"零数量", func(f *Fill) { f.Qty = 0 }},
		{"NaN 价格", func(f *Fill) { f.Price = math.NaN() }},
		{"负佣金", func(f *Fill) { f.Commission = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account, err := NewAccount(11_000)
			if err != nil {
				t.Fatalf("NewAccount: %v", err)
			}
			fill := valid
			tc.mutate(&fill)

			_, _, err = ApplyFill(account, fill)

			if !errors.Is(err, ErrInvalidFill) {
				t.Errorf("err = %v, want ErrInvalidFill", err)
			}
		})
	}
}

func TestApplyFillBuyAddsLotWithCommissionInCost(t *testing.T) {
	// Arrange:现金 11000,买 100 股 @100、佣金 1 → 总成本 10001,每股成本 100.01
	account, _ := NewAccount(11_000)
	before := snapshotAccount(account)
	fill := Fill{Seq: 7, OrderID: 3, Symbol: "AAPL", Side: SideBuy, Qty: 100, Price: 100, Commission: 1, Time: testTime1}

	// Act
	next, entries, err := ApplyFill(account, fill)

	// Assert:账户状态
	if err != nil {
		t.Fatalf("ApplyFill returned unexpected error: %v", err)
	}
	if next.Cash() != 999 {
		t.Errorf("cash = %v, want 999", next.Cash())
	}
	if next.PositionQty("AAPL") != 100 {
		t.Errorf("PositionQty = %v, want 100", next.PositionQty("AAPL"))
	}
	if !almostEqual(next.CostBasis("AAPL"), 10_001) {
		t.Errorf("CostBasis = %v, want 10001", next.CostBasis("AAPL"))
	}

	// Assert:lot 字段(队尾追加,佣金摊入成本)
	lots := next.positions["AAPL"]
	if len(lots) != 1 {
		t.Fatalf("len(lots) = %d, want 1", len(lots))
	}
	lot := lots[0]
	if lot.RemainQty != 100 || lot.openSeq != 7 || !lot.openTime.Equal(testTime1) {
		t.Errorf("lot = %+v, want {Qty 100, openSeq 7, openTime %v}", lot, testTime1)
	}
	if !almostEqual(lot.costPerShare, 100.01) {
		t.Errorf("costPerShare = %v, want 100.01(佣金已摊入)", lot.costPerShare)
	}

	// Assert:一条 TRADE 条目
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Type != EntryTrade || e.Qty != 100 || e.Price != 100 || e.Commission != 1 || e.CashAfter != 999 || e.FillSeq != 7 {
		t.Errorf("entry = %+v, 字段不符合预期", e)
	}

	// Assert:原账户未被改动
	if !reflect.DeepEqual(before, account) {
		t.Error("ApplyFill 改动了原账户,违反不可变性")
	}
}

func TestApplyFillBuyWithExactCashSucceeds(t *testing.T) {
	// Arrange:现金恰好等于总成本
	account, _ := NewAccount(10_001)
	fill := Fill{Seq: 1, Symbol: "AAPL", Side: SideBuy, Qty: 100, Price: 100, Commission: 1, Time: testTime1}

	// Act
	next, _, err := ApplyFill(account, fill)

	// Assert:cash 扣到 0 是合法的(只有 < 0 才报错)
	if err != nil {
		t.Fatalf("ApplyFill returned unexpected error: %v", err)
	}
	if next.Cash() != 0 {
		t.Errorf("cash = %v, want 0", next.Cash())
	}
}

func TestApplyFillBuyInsufficientCashReturnsErrNegativeCash(t *testing.T) {
	// Arrange:现金 10000 < 总成本 10001
	account, _ := NewAccount(10_000)
	fill := Fill{Seq: 1, Symbol: "AAPL", Side: SideBuy, Qty: 100, Price: 100, Commission: 1, Time: testTime1}

	// Act
	next, entries, err := ApplyFill(account, fill)

	// Assert:报 ErrNegativeCash,返回原账户、无账本条目
	if !errors.Is(err, ErrNegativeCash) {
		t.Errorf("err = %v, want ErrNegativeCash", err)
	}
	if next.Cash() != 10_000 {
		t.Errorf("出错后返回的账户 cash = %v, want 10000(原状态)", next.Cash())
	}
	if entries != nil {
		t.Errorf("出错后返回了 %d 条账本, want nil", len(entries))
	}
}

func TestApplyFillSellEntirePositionDeletesSymbol(t *testing.T) {
	// Arrange:先买 100 @100 佣金 1,再全卖 @110 佣金 1
	account, _ := NewAccount(11_000)
	holding, _, err := ApplyFill(account, Fill{Seq: 1, Symbol: "AAPL", Side: SideBuy, Qty: 100, Price: 100, Commission: 1, Time: testTime1})
	if err != nil {
		t.Fatalf("buy: %v", err)
	}
	sell := Fill{Seq: 2, Symbol: "AAPL", Side: SideSell, Qty: 100, Price: 110, Commission: 1, Time: testTime2}

	// Act
	next, entries, err := ApplyFill(holding, sell)

	// Assert:持仓清空且 key 被删除;现金 = 999 + (100×110 − 1) = 11998
	if err != nil {
		t.Fatalf("ApplyFill returned unexpected error: %v", err)
	}
	if _, ok := next.positions["AAPL"]; ok {
		t.Error("清仓后 positions 里仍残留 AAPL key")
	}
	if next.Cash() != 11_998 {
		t.Errorf("cash = %v, want 11998", next.Cash())
	}

	// Assert:一条 REALIZED_PNL + 一条 TRADE
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	r := entries[0]
	if r.Type != EntryRealizedPnL || r.Qty != 100 || r.Price != 110 || !almostEqual(r.RealizedPnL, 998) || r.LotOpenSeq != 1 || r.FillSeq != 2 {
		t.Errorf("REALIZED 条目 = %+v, 字段不符合预期(realized 应为 10999−10001≈998)", r)
	}
	tr := entries[1]
	if tr.Type != EntryTrade || tr.Qty != 100 || tr.Commission != 1 {
		t.Errorf("TRADE 条目 = %+v, 字段不符合预期", tr)
	}
	// 同一事件所有条目 CashAfter 统一为事件后现金
	for i, e := range entries {
		if e.CashAfter != 11_998 {
			t.Errorf("entries[%d].CashAfter = %v, want 11998", i, e.CashAfter)
		}
	}
}

func TestApplyFillSellPartialKeepsRemainingLot(t *testing.T) {
	// Arrange:买 100 @100 佣金 1(每股成本 100.01),卖 60 @110 免佣
	account, _ := NewAccount(11_000)
	holding, _, _ := ApplyFill(account, Fill{Seq: 5, Symbol: "AAPL", Side: SideBuy, Qty: 100, Price: 100, Commission: 1, Time: testTime1})
	sell := Fill{Seq: 6, Symbol: "AAPL", Side: SideSell, Qty: 60, Price: 110, Commission: 0, Time: testTime2}

	// Act
	next, entries, err := ApplyFill(holding, sell)

	// Assert:剩余 40 股,成本/序号不变;realized = 60×110 − 60×100.01 = 599.4
	if err != nil {
		t.Fatalf("ApplyFill returned unexpected error: %v", err)
	}
	lots := next.positions["AAPL"]
	if len(lots) != 1 {
		t.Fatalf("len(lots) = %d, want 1", len(lots))
	}
	if lots[0].RemainQty != 40 {
		t.Errorf("剩余数量 = %v, want 40", lots[0].RemainQty)
	}
	if !almostEqual(lots[0].costPerShare, 100.01) || lots[0].openSeq != 5 {
		t.Errorf("剩余 lot = %+v, 成本或序号被改动", lots[0])
	}
	if !almostEqual(entries[0].RealizedPnL, 599.4) {
		t.Errorf("realized = %v, want 599.4", entries[0].RealizedPnL)
	}
	if next.Cash() != 999+6_600 {
		t.Errorf("cash = %v, want 7599", next.Cash())
	}
}

// 规格 7.4 的 golden 数值:100@100.01 与 50@120 两个 lot,FIFO 卖出 120@150 佣金 1
func TestApplyFillSellMultipleLotsFIFO(t *testing.T) {
	// Arrange
	account, _ := NewAccount(30_000)
	s1, _, _ := ApplyFill(account, Fill{Seq: 1, Symbol: "AAPL", Side: SideBuy, Qty: 100, Price: 100, Commission: 1, Time: testTime1})
	s2, _, _ := ApplyFill(s1, Fill{Seq: 2, Symbol: "AAPL", Side: SideBuy, Qty: 50, Price: 120, Commission: 0, Time: testTime1})
	sell := Fill{Seq: 3, Symbol: "AAPL", Side: SideSell, Qty: 120, Price: 150, Commission: 1, Time: testTime2}

	// Act
	next, entries, err := ApplyFill(s2, sell)

	// Assert:netProceeds = 120×150 − 1 = 17999
	if err != nil {
		t.Fatalf("ApplyFill returned unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3(两条 REALIZED + 一条 TRADE)", len(entries))
	}
	// lot1 全消耗:分摊 17999×100/120 = 14999.1667,realized ≈ 4998.1667
	if entries[0].LotOpenSeq != 1 || entries[0].Qty != 100 || !almostEqual(entries[0].RealizedPnL, 4998.166666666667) {
		t.Errorf("REALIZED[0] = %+v, want lotOpenSeq=1 qty=100 realized≈4998.1667", entries[0])
	}
	// lot2 消耗 20(末笔吸收残差):分摊 2999.8333,realized ≈ 599.8333
	if entries[1].LotOpenSeq != 2 || entries[1].Qty != 20 || !almostEqual(entries[1].RealizedPnL, 599.8333333333333) {
		t.Errorf("REALIZED[1] = %+v, want lotOpenSeq=2 qty=20 realized≈599.8333", entries[1])
	}
	if entries[2].Type != EntryTrade || entries[2].Qty != 120 {
		t.Errorf("entries[2] = %+v, want TRADE qty=120", entries[2])
	}
	// 剩余 lot2:30 @120
	lots := next.positions["AAPL"]
	if len(lots) != 1 || lots[0].RemainQty != 30 || lots[0].costPerShare != 120 {
		t.Errorf("剩余 lots = %+v, want [30 @120]", lots)
	}
}

func TestApplyFillOversellReturnsErrOversell(t *testing.T) {
	// Arrange:持仓 100,卖 101
	account, _ := NewAccount(11_000)
	holding, _, _ := ApplyFill(account, Fill{Seq: 1, Symbol: "AAPL", Side: SideBuy, Qty: 100, Price: 100, Commission: 1, Time: testTime1})
	before := snapshotAccount(holding)
	sell := Fill{Seq: 2, Symbol: "AAPL", Side: SideSell, Qty: 101, Price: 110, Commission: 0, Time: testTime2}

	// Act
	next, entries, err := ApplyFill(holding, sell)

	// Assert
	if !errors.Is(err, ErrOversell) {
		t.Errorf("err = %v, want ErrOversell", err)
	}
	if entries != nil {
		t.Errorf("出错后返回了 %d 条账本, want nil", len(entries))
	}
	if next.PositionQty("AAPL") != 100 {
		t.Errorf("出错后持仓 = %v, want 100(原状态)", next.PositionQty("AAPL"))
	}
	if !reflect.DeepEqual(before, holding) {
		t.Error("ApplyFill 改动了原账户,违反不可变性")
	}
}
