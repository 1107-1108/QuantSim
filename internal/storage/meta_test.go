package storage

import (
	"FinanceSimulator/datasource"
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestOpenMetaStore 创建数据库、应用首个 migration，并在重开时保留 schema。
func TestOpenMetaStore(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "data", "app.db")
	store, err := OpenMetaStore(databasePath)
	if err != nil {
		t.Fatalf("OpenMetaStore: %v", err)
	}

	for _, tableName := range []string{
		"schema_migrations",
		"symbols",
		"trading_calendar",
		"corporate_actions",
		"import_runs",
	} {
		var found int
		if err := store.db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
			tableName,
		).Scan(&found); err != nil {
			t.Fatalf("query table %q: %v", tableName, err)
		}
		if found != 1 {
			t.Errorf("table %q count = %d, want 1", tableName, found)
		}
	}

	var foreignKeys int
	if err := store.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}

	var journalMode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode pragma: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopenedStore, err := OpenMetaStore(databasePath)
	if err != nil {
		t.Fatalf("reopen MetaStore: %v", err)
	}
	t.Cleanup(func() {
		if err := reopenedStore.Close(); err != nil {
			t.Errorf("close reopened store: %v", err)
		}
	})

	var migrationCount int
	if err := reopenedStore.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&migrationCount); err != nil {
		t.Fatalf("query migration: %v", err)
	}
	if migrationCount != 1 {
		t.Errorf("migration version 1 count = %d, want 1", migrationCount)
	}
}

// TestMetaSymbols 验证 SymbolInfo 的可选日期能够 round-trip，并以 symbol 作为 upsert 键。
func TestMetaSymbols(t *testing.T) {
	store := openTestMetaStore(t)
	context := context.Background()
	delistDate := time.Date(2030, 6, 1, 0, 0, 0, 0, time.UTC)
	symbol := datasource.SymbolInfo{
		Symbol:     "AAPL",
		Name:       "Apple Inc.",
		Exchange:   "NASDAQ",
		Currency:   "USD",
		AssetType:  datasource.AssetStock,
		ListDate:   time.Date(1980, 12, 12, 0, 0, 0, 0, time.UTC),
		DelistDate: &delistDate,
		Timezone:   "America/New_York",
		LotSize:    1,
		TickSize:   0.01,
		ISIN:       "US0378331005",
	}

	if err := store.UpsertSymbol(context, symbol); err != nil {
		t.Fatalf("UpsertSymbol: %v", err)
	}
	symbol.Name = "Apple Inc. (updated)"
	if err := store.UpsertSymbol(context, symbol); err != nil {
		t.Fatalf("update UpsertSymbol: %v", err)
	}

	symbols, err := store.ListSymbols(context)
	if err != nil {
		t.Fatalf("ListSymbols: %v", err)
	}
	if !reflect.DeepEqual(symbols, []datasource.SymbolInfo{symbol}) {
		t.Errorf("ListSymbols = %#v, want %#v", symbols, []datasource.SymbolInfo{symbol})
	}

	symbolWithoutOptionalDates := datasource.SymbolInfo{
		Symbol:    "MSFT",
		Name:      "Microsoft Corporation",
		Exchange:  "NASDAQ",
		Currency:  "USD",
		AssetType: datasource.AssetStock,
		Timezone:  "America/New_York",
		LotSize:   1,
		TickSize:  0.01,
	}
	if err := store.UpsertSymbol(context, symbolWithoutOptionalDates); err != nil {
		t.Fatalf("UpsertSymbol without optional dates: %v", err)
	}

	symbols, err = store.ListSymbols(context)
	if err != nil {
		t.Fatalf("ListSymbols with optional dates: %v", err)
	}
	if !reflect.DeepEqual(symbols, []datasource.SymbolInfo{symbol, symbolWithoutOptionalDates}) {
		t.Errorf("ListSymbols optional dates = %#v, want %#v", symbols, []datasource.SymbolInfo{symbol, symbolWithoutOptionalDates})
	}
}

// TestMetaTradingDays 验证交易日与嵌套交易时段按时间范围 round-trip。
func TestMetaTradingDays(t *testing.T) {
	store := openTestMetaStore(t)
	context := context.Background()
	tradingDay := datasource.TradingDay{
		Date:         time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		Exchange:     "NASDAQ",
		IsTradingDay: true,
		Sessions: []datasource.TradeSession{
			{
				Open:  time.Date(2024, 1, 2, 14, 30, 0, 0, time.UTC),
				Close: time.Date(2024, 1, 2, 21, 0, 0, 0, time.UTC),
			},
		},
	}

	if err := store.UpsertTradingDays(context, []datasource.TradingDay{tradingDay}); err != nil {
		t.Fatalf("UpsertTradingDays: %v", err)
	}

	days, err := store.ListTradingDays(
		context,
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("ListTradingDays: %v", err)
	}
	if !reflect.DeepEqual(days, []datasource.TradingDay{tradingDay}) {
		t.Errorf("ListTradingDays = %#v, want %#v", days, []datasource.TradingDay{tradingDay})
	}
}

// TestMetaCorporateActions 验证公司行为按自然键 upsert，且查询保留最新 source。
func TestMetaCorporateActions(t *testing.T) {
	store := openTestMetaStore(t)
	context := context.Background()
	dividend := datasource.CorporateAction{
		Symbol:       "AAPL",
		Type:         datasource.ActionDividend,
		Date:         time.Date(2024, 2, 9, 0, 0, 0, 0, time.UTC),
		CashPerShare: 0.24,
	}
	split := datasource.CorporateAction{
		Symbol:     "AAPL",
		Type:       datasource.ActionSplit,
		Date:       time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC),
		SplitRatio: 4,
	}

	if err := store.UpsertCorporateActions(context, "yahoo", []datasource.CorporateAction{split, dividend}); err != nil {
		t.Fatalf("UpsertCorporateActions: %v", err)
	}
	if err := store.UpsertCorporateActions(context, "manual", []datasource.CorporateAction{dividend}); err != nil {
		t.Fatalf("update UpsertCorporateActions: %v", err)
	}

	actions, err := store.ListCorporateActions(
		context,
		"AAPL",
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("ListCorporateActions: %v", err)
	}
	want := []StoredCorporateAction{
		{CorporateAction: dividend, Source: "manual"},
		{CorporateAction: split, Source: "yahoo"},
	}
	if !reflect.DeepEqual(actions, want) {
		t.Errorf("ListCorporateActions = %#v, want %#v", actions, want)
	}
}

// TestImportRunLifecycle 验证导入任务的创建、完成与失败状态能够可靠持久化。
func TestImportRunLifecycle(t *testing.T) {
	store := openTestMetaStore(t)
	context := context.Background()
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	runningRun, err := store.CreateImportRun(context, "AAPL", datasource.TF1d, from, to)
	if err != nil {
		t.Fatalf("CreateImportRun: %v", err)
	}
	if runningRun.ID == "" {
		t.Fatal("CreateImportRun ID is empty")
	}
	if runningRun.Status != ImportRunRunning {
		t.Errorf("CreateImportRun status = %q, want %q", runningRun.Status, ImportRunRunning)
	}
	if runningRun.StartedAt.IsZero() || runningRun.FinishedAt != nil {
		t.Errorf("CreateImportRun timestamps = started %v, finished %v, want started and nil", runningRun.StartedAt, runningRun.FinishedAt)
	}

	const completedIssues = `[{"level":"warning","message":"one missing trading day"}]`
	if err := store.CompleteImportRun(context, runningRun.ID, completedIssues); err != nil {
		t.Fatalf("CompleteImportRun: %v", err)
	}
	completedRun, err := store.GetImportRun(context, runningRun.ID)
	if err != nil {
		t.Fatalf("GetImportRun completed: %v", err)
	}
	if completedRun.Status != ImportRunCompleted || completedRun.FinishedAt == nil || completedRun.IssuesJSON != completedIssues || completedRun.ErrorMessage != "" {
		t.Errorf("completed ImportRun = %#v, want completed run with issues and finished time", completedRun)
	}
	if !completedRun.StartedAt.Equal(runningRun.StartedAt) {
		t.Errorf("completed ImportRun StartedAt = %v, want %v", completedRun.StartedAt, runningRun.StartedAt)
	}

	failedRun, err := store.CreateImportRun(context, "MSFT", datasource.TF1d, from, to)
	if err != nil {
		t.Fatalf("CreateImportRun failed fixture: %v", err)
	}
	const failedIssues = `[{"level":"error","message":"bad source payload"}]`
	if err := store.FailImportRun(context, failedRun.ID, "Yahoo returned 429", failedIssues); err != nil {
		t.Fatalf("FailImportRun: %v", err)
	}
	storedFailedRun, err := store.GetImportRun(context, failedRun.ID)
	if err != nil {
		t.Fatalf("GetImportRun failed: %v", err)
	}
	if storedFailedRun.Status != ImportRunFailed || storedFailedRun.FinishedAt == nil || storedFailedRun.IssuesJSON != failedIssues || storedFailedRun.ErrorMessage != "Yahoo returned 429" {
		t.Errorf("failed ImportRun = %#v, want failed run with error, issues, and finished time", storedFailedRun)
	}
}

// openTestMetaStore 创建会在测试结束时关闭的临时 MetaStore。
func openTestMetaStore(t *testing.T) *MetaStore {
	t.Helper()
	store, err := OpenMetaStore(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("OpenMetaStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close MetaStore: %v", err)
		}
	})
	return store
}
