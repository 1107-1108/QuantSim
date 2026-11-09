package portfolio

import (
	"math/rand"
	"reflect"
	"testing"

	"FinanceSimulator/datasource"
)

// 手工脚本序列:买 → 买 → 部分卖出,逐笔累积账本,最后跑守恒校验
func TestVerifyConservationAfterTradeSequence(t *testing.T) {
	// Arrange & Act:30000 初始;买 100@100+1;买 50@120+0;卖 120@150+1
	account, _ := NewAccount(30_000)
	var entries []LedgerEntry

	apply := func(acc Account, fill Fill) Account {
		next, newEntries, applyErr := ApplyFill(acc, fill)
		if applyErr != nil {
			t.Fatalf("ApplyFill: %v", applyErr)
		}
		entries = append(entries, newEntries...)
		return next
	}

	account = apply(account, Fill{Seq: 1, Symbol: "AAPL", Side: SideBuy, Qty: 100, Price: 100, Commission: 1, Time: testTime1})
	account = apply(account, Fill{Seq: 2, Symbol: "AAPL", Side: SideBuy, Qty: 50, Price: 120, Commission: 0, Time: testTime1})
	account = apply(account, Fill{Seq: 3, Symbol: "AAPL", Side: SideSell, Qty: 120, Price: 150, Commission: 1, Time: testTime2})

	// Assert:现金 + 成本基础 = 初始现金 + 已实现
	if err := VerifyConservation(30_000, account, entries); err != nil {
		t.Errorf("VerifyConservation: %v", err)
	}
}

// TestVerifyConservationAcrossRandomEventSequences 用固定随机序列验证混合事件后的守恒与不可变性。
func TestVerifyConservationAcrossRandomEventSequences(t *testing.T) {
	const initialCash = 10_000.0
	account, _ := NewAccount(initialCash)
	entries := []LedgerEntry(nil)
	random := rand.New(rand.NewSource(42))
	sequence := uint64(1)

	for step := 0; step < 100; step++ {
		before := snapshotAccount(account)
		source := account
		eventTime := testTime1.AddDate(0, 0, step)

		switch random.Intn(4) {
		case 0:
			price := float64(random.Intn(20) + 1)
			qty := float64(random.Intn(5) + 1)
			if price*qty > source.Cash() {
				continue
			}
			next, newEntries, err := ApplyFill(source, Fill{Seq: sequence, Symbol: "AAPL", Side: SideBuy, Qty: qty, Price: price, Time: eventTime})
			if err != nil {
				t.Fatalf("step %d buy: %v", step, err)
			}
			sequence++
			account, entries = next, append(entries, newEntries...)
		case 1:
			held := source.PositionQty("AAPL")
			if held == 0 {
				continue
			}
			qty := held * float64(random.Intn(100)+1) / 100
			price := float64(random.Intn(20) + 1)
			next, newEntries, err := ApplyFill(source, Fill{Seq: sequence, Symbol: "AAPL", Side: SideSell, Qty: qty, Price: price, Time: eventTime})
			if err != nil {
				t.Fatalf("step %d sell: %v", step, err)
			}
			sequence++
			account, entries = next, append(entries, newEntries...)
		case 2:
			action := datasource.CorporateAction{Symbol: "AAPL", Type: datasource.ActionSplit, SplitRatio: 2, Date: eventTime}
			if random.Intn(2) == 0 {
				action.Type = datasource.ActionReverseSplit
				action.SplitRatio = 0.5
			}
			next, newEntries, err := ApplyCorporateAction(source, action)
			if err != nil {
				t.Fatalf("step %d split: %v", step, err)
			}
			account, entries = next, append(entries, newEntries...)
		case 3:
			next, newEntries, err := ApplyCorporateAction(source, datasource.CorporateAction{
				Symbol:       "AAPL",
				Type:         datasource.ActionDividend,
				CashPerShare: 0.1,
				Date:         eventTime,
			})
			if err != nil {
				t.Fatalf("step %d dividend: %v", step, err)
			}
			account, entries = next, append(entries, newEntries...)
		}

		if !reflect.DeepEqual(before, source) {
			t.Fatalf("step %d changed the source account", step)
		}
		if err := VerifyConservation(initialCash, account, entries); err != nil {
			t.Fatalf("step %d conservation: %v", step, err)
		}
	}
}

// TestVerifyConservationAfterMixedEvents 验证成交、拆股和分红混合后守恒关系仍成立。
func TestVerifyConservationAfterMixedEvents(t *testing.T) {
	account, _ := NewAccount(5_000)
	var entries []LedgerEntry

	applyFill := func(fill Fill) {
		next, newEntries, err := ApplyFill(account, fill)
		if err != nil {
			t.Fatalf("ApplyFill(%+v): %v", fill, err)
		}
		account = next
		entries = append(entries, newEntries...)
	}
	applyAction := func(action datasource.CorporateAction) {
		next, newEntries, err := ApplyCorporateAction(account, action)
		if err != nil {
			t.Fatalf("ApplyCorporateAction(%+v): %v", action, err)
		}
		account = next
		entries = append(entries, newEntries...)
	}

	applyFill(Fill{Seq: 1, Symbol: "AAPL", Side: SideBuy, Qty: 100, Price: 10, Time: testTime1})
	applyAction(datasource.CorporateAction{Symbol: "AAPL", Type: datasource.ActionSplit, SplitRatio: 2, Date: testTime2})
	applyAction(datasource.CorporateAction{Symbol: "AAPL", Type: datasource.ActionDividend, CashPerShare: 0.5, Date: testTime2})
	applyFill(Fill{Seq: 2, Symbol: "AAPL", Side: SideSell, Qty: 200, Price: 6, Time: testTime2})

	if err := VerifyConservation(5_000, account, entries); err != nil {
		t.Errorf("VerifyConservation after mixed events: %v", err)
	}
}

func TestVerifyConservationDetectsImbalance(t *testing.T) {
	// Arrange:账户现金 1000 无持仓,但声称初始现金是 2000 → 左右必然不等
	account, _ := NewAccount(1_000)

	// Act
	err := VerifyConservation(2_000, account, nil)

	// Assert
	if err == nil {
		t.Error("初始现金对不上时 VerifyConservation 应报错")
	}
}

// 不依赖 corporate.go,手工构造 DIVIDEND 条目验证守恒公式的分红项
func TestVerifyConservationIncludesDividends(t *testing.T) {
	// Arrange:初始 1000,分红 400 股 × 0.5 = 200 入账 → 现金 1200
	account, _ := NewAccount(1_200)
	entries := []LedgerEntry{
		{Type: EntryDividend, Symbol: "AAPL", Qty: 400, Amount: 0.5, CashAfter: 1_200},
	}

	// Act & Assert
	if err := VerifyConservation(1_000, account, entries); err != nil {
		t.Errorf("VerifyConservation: %v", err)
	}
}
