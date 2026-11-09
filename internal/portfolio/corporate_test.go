package portfolio

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"FinanceSimulator/datasource"
)

// accountWithHolding 创建已完成买入记账的账户，用于公司行为的真实状态转换测试。
func accountWithHolding(t *testing.T, cash, qty, price float64) Account {
	t.Helper()
	account, err := NewAccount(cash)
	if err != nil {
		t.Fatalf("NewAccount: %v", err)
	}
	next, _, err := ApplyFill(account, Fill{Seq: 1, Symbol: "AAPL", Side: SideBuy, Qty: qty, Price: price, Time: testTime1})
	if err != nil {
		t.Fatalf("buy holding: %v", err)
	}
	return next
}

// TestApplyCorporateActionAppliesSplitWithoutChangingCash 验证拆股只调整 lot 结构与账本。
func TestApplyCorporateActionAppliesSplitWithoutChangingCash(t *testing.T) {
	account := accountWithHolding(t, 2_000, 100, 10)
	before := snapshotAccount(account)

	next, entries, err := ApplyCorporateAction(account, datasource.CorporateAction{
		Symbol:     "AAPL",
		Type:       datasource.ActionSplit,
		Date:       testTime2,
		SplitRatio: 4,
	})

	if err != nil {
		t.Fatalf("ApplyCorporateAction: %v", err)
	}
	if next.Cash() != 1_000 || next.PositionQty("AAPL") != 400 {
		t.Errorf("post-split cash/qty = %v/%v, want 1000/400", next.Cash(), next.PositionQty("AAPL"))
	}
	if lot := next.positions["AAPL"][0]; lot.CostPerShare != 2.5 || lot.Qty != 400 {
		t.Errorf("split lot = %+v, want Qty 400 and CostPerShare 2.5", lot)
	}
	if len(entries) != 1 || entries[0].Type != EntrySplitAdjustment || entries[0].Qty != 400 || entries[0].Amount != 4 || entries[0].CashAfter != 1_000 {
		t.Errorf("split entries = %+v, want one split adjustment", entries)
	}
	if !reflect.DeepEqual(before, account) {
		t.Error("split changed the input account")
	}
}

// TestApplyCorporateActionAppliesHistoricalStockSplits 用已发生的 AAPL 和 TSLA 拆股比例做回归锚点。
func TestApplyCorporateActionAppliesHistoricalStockSplits(t *testing.T) {
	cases := []struct {
		name  string
		ratio float64
	}{
		{name: "AAPL 2020-08-31 4:1", ratio: 4},
		{name: "TSLA 2022-08-25 3:1", ratio: 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := accountWithHolding(t, 2_000, 10, 100)
			next, _, err := ApplyCorporateAction(account, datasource.CorporateAction{
				Symbol:     "AAPL",
				Type:       datasource.ActionSplit,
				Date:       testTime2,
				SplitRatio: tc.ratio,
			})

			if err != nil {
				t.Fatalf("ApplyCorporateAction: %v", err)
			}
			lot := next.positions["AAPL"][0]
			if lot.Qty != 10*tc.ratio || lot.CostPerShare != 100/tc.ratio {
				t.Errorf("split lot = %+v, want Qty %v and CostPerShare %v", lot, 10*tc.ratio, 100/tc.ratio)
			}
		})
	}
}

// TestApplyCorporateActionPreservesReverseSplitFractions 验证反向拆股保留碎股而不强制现金结算。
func TestApplyCorporateActionPreservesReverseSplitFractions(t *testing.T) {
	account := accountWithHolding(t, 2_000, 103, 10)

	next, entries, err := ApplyCorporateAction(account, datasource.CorporateAction{
		Symbol:     "AAPL",
		Type:       datasource.ActionReverseSplit,
		Date:       testTime2,
		SplitRatio: 0.2,
	})

	if err != nil {
		t.Fatalf("ApplyCorporateAction: %v", err)
	}
	lot := next.positions["AAPL"][0]
	if math.Abs(lot.Qty-20.6) > 1e-9 || lot.CostPerShare != 50 {
		t.Errorf("reverse-split lot = %+v, want Qty 20.6 and CostPerShare 50", lot)
	}
	if len(entries) != 1 || entries[0].Amount != 0.2 || math.Abs(entries[0].Qty-20.6) > 1e-9 {
		t.Errorf("reverse-split entries = %+v, want one adjustment for 20.6 shares", entries)
	}
}

// TestApplyCorporateActionBooksDividend 验证除息日的持仓数量决定直接入账的现金分红。
func TestApplyCorporateActionBooksDividend(t *testing.T) {
	account := accountWithHolding(t, 5_000, 400, 10)

	next, entries, err := ApplyCorporateAction(account, datasource.CorporateAction{
		Symbol:       "AAPL",
		Type:         datasource.ActionDividend,
		Date:         testTime2,
		CashPerShare: 0.5,
	})

	if err != nil {
		t.Fatalf("ApplyCorporateAction: %v", err)
	}
	if next.Cash() != 1_200 {
		t.Errorf("cash after dividend = %v, want 1200", next.Cash())
	}
	if len(entries) != 1 || entries[0].Type != EntryDividend || entries[0].Qty != 400 || entries[0].Amount != 0.5 || entries[0].CashAfter != 1_200 {
		t.Errorf("dividend entries = %+v, want one dividend entry", entries)
	}
}

// TestApplyCorporateActionReturnsNoOpForNoPosition 验证无持仓公司行为不创建无意义账本记录。
func TestApplyCorporateActionReturnsNoOpForNoPosition(t *testing.T) {
	account, _ := NewAccount(1_000)

	for _, action := range []datasource.CorporateAction{
		{Symbol: "AAPL", Type: datasource.ActionSplit, SplitRatio: 4},
		{Symbol: "AAPL", Type: datasource.ActionDividend, CashPerShare: 0.5},
	} {
		next, entries, err := ApplyCorporateAction(account, action)
		if err != nil {
			t.Fatalf("ApplyCorporateAction(%s): %v", action.Type, err)
		}
		if !reflect.DeepEqual(next, account) || entries != nil {
			t.Errorf("no-position %s = account %+v, entries %v; want unchanged account and nil entries", action.Type, next, entries)
		}
	}
}

// TestApplyCorporateActionRejectsUnknownOrInvalidActions 防止未知类型和非法比例悄悄损坏账户。
func TestApplyCorporateActionRejectsUnknownOrInvalidActions(t *testing.T) {
	account := accountWithHolding(t, 2_000, 100, 10)
	before := snapshotAccount(account)
	cases := []datasource.CorporateAction{
		{Symbol: "AAPL", Type: "SPINOFF"},
		{Symbol: "AAPL", Type: datasource.ActionSplit, SplitRatio: 0},
		{Symbol: "AAPL", Type: datasource.ActionReverseSplit, SplitRatio: math.NaN()},
	}

	for _, action := range cases {
		next, entries, err := ApplyCorporateAction(account, action)
		if !errors.Is(err, ErrUnknownCorporateAction) {
			t.Errorf("ApplyCorporateAction(%+v) err = %v, want ErrUnknownCorporateAction", action, err)
		}
		if entries != nil || !reflect.DeepEqual(next, before) || !reflect.DeepEqual(account, before) {
			t.Errorf("invalid action %+v changed account or returned entries", action)
		}
	}
}
