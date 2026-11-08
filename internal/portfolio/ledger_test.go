package portfolio

import (
	"testing"
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
