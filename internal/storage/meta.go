package storage

import (
	"FinanceSimulator/datasource"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

const metadataSchemaVersion = 1

// MetaStore 管理 SQLite 中的业务元数据与在线状态。
// M1 只包含行情元数据和导入记录，不包含回测或 paper trading 的 repository。
type MetaStore struct {
	db *sql.DB
}

// StoredCorporateAction 是附带数据源标记的公司行为记录。
type StoredCorporateAction struct {
	datasource.CorporateAction
	Source string
}

// ImportRunStatus 表示一次历史行情导入的生命周期状态。
type ImportRunStatus string

const (
	// ImportRunRunning 表示导入任务正在执行。
	ImportRunRunning ImportRunStatus = "running"

	// ImportRunCompleted 表示导入任务已成功完成。
	ImportRunCompleted ImportRunStatus = "completed"

	// ImportRunFailed 表示导入任务因错误结束。
	ImportRunFailed ImportRunStatus = "failed"
)

// ImportRun 记录一次历史行情导入的范围、状态与数据质量结果。
type ImportRun struct {
	ID           string
	Symbol       string
	Timeframe    datasource.Timeframe
	From         time.Time
	To           time.Time
	StartedAt    time.Time
	FinishedAt   *time.Time
	Status       ImportRunStatus
	IssuesJSON   string
	ErrorMessage string
}

// OpenMetaStore 打开 path 指向的 SQLite 数据库，并应用所有尚未执行的 migration。
func OpenMetaStore(path string) (*MetaStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("SQLite database path cannot be empty")
	}

	if directory := filepath.Dir(path); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, fmt.Errorf("create SQLite data directory: %w", err)
		}
	}

	database, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	database.SetMaxOpenConns(1)

	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("connect to SQLite database: %w", err)
	}

	store := &MetaStore{db: database}
	if err := store.configureAndMigrate(); err != nil {
		_ = database.Close()
		return nil, err
	}
	return store, nil
}

// Close 关闭 MetaStore 持有的 SQLite 数据库连接。
func (store *MetaStore) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

// UpsertSymbol 创建或更新一条证券元数据记录。
func (store *MetaStore) UpsertSymbol(ctx context.Context, symbol datasource.SymbolInfo) error {
	_, err := store.db.ExecContext(ctx, `
		INSERT INTO symbols(
			symbol, name, exchange, currency, asset_type, list_date, delist_date,
			timezone, lot_size, tick_size, isin
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(symbol) DO UPDATE SET
			name = excluded.name,
			exchange = excluded.exchange,
			currency = excluded.currency,
			asset_type = excluded.asset_type,
			list_date = excluded.list_date,
			delist_date = excluded.delist_date,
			timezone = excluded.timezone,
			lot_size = excluded.lot_size,
			tick_size = excluded.tick_size,
			isin = excluded.isin
	`,
		symbol.Symbol,
		symbol.Name,
		symbol.Exchange,
		symbol.Currency,
		string(symbol.AssetType),
		nullableTimeValue(symbol.ListDate),
		nullableTimePointerValue(symbol.DelistDate),
		symbol.Timezone,
		symbol.LotSize,
		symbol.TickSize,
		symbol.ISIN,
	)
	if err != nil {
		return fmt.Errorf("upsert symbol %q: %w", symbol.Symbol, err)
	}
	return nil
}

// ListSymbols 返回按股票代码升序排列的全部证券元数据。
func (store *MetaStore) ListSymbols(ctx context.Context) ([]datasource.SymbolInfo, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT symbol, name, exchange, currency, asset_type, list_date, delist_date,
			timezone, lot_size, tick_size, isin
		FROM symbols
		ORDER BY symbol
	`)
	if err != nil {
		return nil, fmt.Errorf("query symbol metadata: %w", err)
	}
	defer rows.Close()

	symbols := make([]datasource.SymbolInfo, 0)
	for rows.Next() {
		var symbol datasource.SymbolInfo
		var assetType string
		var listDate sql.NullInt64
		var delistDate sql.NullInt64
		if err := rows.Scan(
			&symbol.Symbol,
			&symbol.Name,
			&symbol.Exchange,
			&symbol.Currency,
			&assetType,
			&listDate,
			&delistDate,
			&symbol.Timezone,
			&symbol.LotSize,
			&symbol.TickSize,
			&symbol.ISIN,
		); err != nil {
			return nil, fmt.Errorf("scan symbol metadata: %w", err)
		}
		symbol.AssetType = datasource.AssetType(assetType)
		symbol.ListDate = timeFromNullableValue(listDate)
		symbol.DelistDate = timePointerFromNullableValue(delistDate)
		symbols = append(symbols, symbol)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate symbol metadata: %w", err)
	}
	return symbols, nil
}

// UpsertTradingDays 创建或更新一批交易日历记录。
func (store *MetaStore) UpsertTradingDays(ctx context.Context, days []datasource.TradingDay) error {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin trading calendar transaction: %w", err)
	}
	defer transaction.Rollback()

	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO trading_calendar(date, exchange, is_trading_day, sessions_json, holiday_name)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			exchange = excluded.exchange,
			is_trading_day = excluded.is_trading_day,
			sessions_json = excluded.sessions_json,
			holiday_name = excluded.holiday_name
	`)
	if err != nil {
		return fmt.Errorf("prepare trading calendar upsert: %w", err)
	}
	defer statement.Close()

	for _, day := range days {
		sessionsJSON, err := json.Marshal(day.Sessions)
		if err != nil {
			return fmt.Errorf("marshal trading sessions for %s: %w", day.Date.Format(time.DateOnly), err)
		}
		isTradingDay := 0
		if day.IsTradingDay {
			isTradingDay = 1
		}
		if _, err := statement.ExecContext(
			ctx,
			day.Date.UTC().UnixMicro(),
			day.Exchange,
			isTradingDay,
			string(sessionsJSON),
			day.HolidayName,
		); err != nil {
			return fmt.Errorf("upsert trading day %s: %w", day.Date.Format(time.DateOnly), err)
		}
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit trading calendar transaction: %w", err)
	}
	return nil
}

// ListTradingDays 返回 [from, to) 范围内按日期升序排列的交易日历记录。
func (store *MetaStore) ListTradingDays(ctx context.Context, from, to time.Time) ([]datasource.TradingDay, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT date, exchange, is_trading_day, sessions_json, holiday_name
		FROM trading_calendar
		WHERE date >= ? AND date < ?
		ORDER BY date
	`, from.UTC().UnixMicro(), to.UTC().UnixMicro())
	if err != nil {
		return nil, fmt.Errorf("query trading calendar: %w", err)
	}
	defer rows.Close()

	days := make([]datasource.TradingDay, 0)
	for rows.Next() {
		var day datasource.TradingDay
		var date int64
		var isTradingDay int
		var sessionsJSON string
		if err := rows.Scan(&date, &day.Exchange, &isTradingDay, &sessionsJSON, &day.HolidayName); err != nil {
			return nil, fmt.Errorf("scan trading calendar: %w", err)
		}
		if err := json.Unmarshal([]byte(sessionsJSON), &day.Sessions); err != nil {
			return nil, fmt.Errorf("unmarshal trading sessions: %w", err)
		}
		day.Date = time.UnixMicro(date).UTC()
		day.IsTradingDay = isTradingDay != 0
		days = append(days, day)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trading calendar: %w", err)
	}
	return days, nil
}

// UpsertCorporateActions 创建或更新同一证券、除息日和类型的公司行为记录。
func (store *MetaStore) UpsertCorporateActions(ctx context.Context, source string, actions []datasource.CorporateAction) error {
	if strings.TrimSpace(source) == "" {
		return fmt.Errorf("corporate action source cannot be empty")
	}

	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin corporate action transaction: %w", err)
	}
	defer transaction.Rollback()

	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO corporate_actions(
			symbol, action_type, ex_date, cash_per_share, split_ratio, source
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(symbol, ex_date, action_type) DO UPDATE SET
			cash_per_share = excluded.cash_per_share,
			split_ratio = excluded.split_ratio,
			source = excluded.source
	`)
	if err != nil {
		return fmt.Errorf("prepare corporate action upsert: %w", err)
	}
	defer statement.Close()

	for _, action := range actions {
		if _, err := statement.ExecContext(
			ctx,
			action.Symbol,
			string(action.Type),
			action.Date.UTC().UnixMicro(),
			action.CashPerShare,
			action.SplitRatio,
			source,
		); err != nil {
			return fmt.Errorf("upsert corporate action for %s: %w", action.Symbol, err)
		}
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit corporate action transaction: %w", err)
	}
	return nil
}

// ListCorporateActions 返回 [from, to) 范围内指定证券按除息日升序排列的公司行为。
func (store *MetaStore) ListCorporateActions(ctx context.Context, symbol string, from, to time.Time) ([]StoredCorporateAction, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT symbol, action_type, ex_date, cash_per_share, split_ratio, source
		FROM corporate_actions
		WHERE symbol = ? AND ex_date >= ? AND ex_date < ?
		ORDER BY ex_date, action_type
	`, symbol, from.UTC().UnixMicro(), to.UTC().UnixMicro())
	if err != nil {
		return nil, fmt.Errorf("query corporate actions for %q: %w", symbol, err)
	}
	defer rows.Close()

	actions := make([]StoredCorporateAction, 0)
	for rows.Next() {
		var action StoredCorporateAction
		var actionType string
		var date int64
		if err := rows.Scan(
			&action.Symbol,
			&actionType,
			&date,
			&action.CashPerShare,
			&action.SplitRatio,
			&action.Source,
		); err != nil {
			return nil, fmt.Errorf("scan corporate action: %w", err)
		}
		action.Type = datasource.CorporateActionType(actionType)
		action.Date = time.UnixMicro(date).UTC()
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate corporate actions: %w", err)
	}
	return actions, nil
}

// CreateImportRun 创建一条 running 状态的历史行情导入记录。
func (store *MetaStore) CreateImportRun(ctx context.Context, symbol string, timeframe datasource.Timeframe, from, to time.Time) (ImportRun, error) {
	run := ImportRun{
		ID:         uuid.NewString(),
		Symbol:     symbol,
		Timeframe:  timeframe,
		From:       from.UTC(),
		To:         to.UTC(),
		StartedAt:  time.Now().UTC().Truncate(time.Microsecond),
		Status:     ImportRunRunning,
		IssuesJSON: "[]",
	}

	_, err := store.db.ExecContext(ctx, `
		INSERT INTO import_runs(
			id, symbol, timeframe, from_time, to_time, started_at, finished_at,
			status, issues_json, error_message
		) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)
	`,
		run.ID,
		run.Symbol,
		string(run.Timeframe),
		run.From.UnixMicro(),
		run.To.UnixMicro(),
		run.StartedAt.UnixMicro(),
		string(run.Status),
		run.IssuesJSON,
		run.ErrorMessage,
	)
	if err != nil {
		return ImportRun{}, fmt.Errorf("create import run: %w", err)
	}
	return run, nil
}

// CompleteImportRun 将导入记录标记为完成并保存数据质量问题 JSON。
func (store *MetaStore) CompleteImportRun(ctx context.Context, id, issuesJSON string) error {
	return store.finishImportRun(ctx, id, ImportRunCompleted, issuesJSON, "")
}

// FailImportRun 将导入记录标记为失败并保存错误和数据质量问题 JSON。
func (store *MetaStore) FailImportRun(ctx context.Context, id, errorMessage, issuesJSON string) error {
	return store.finishImportRun(ctx, id, ImportRunFailed, issuesJSON, errorMessage)
}

// GetImportRun 按 ID 读取一条导入记录。
func (store *MetaStore) GetImportRun(ctx context.Context, id string) (ImportRun, error) {
	var run ImportRun
	var timeframe string
	var from int64
	var to int64
	var startedAt int64
	var finishedAt sql.NullInt64
	var status string
	if err := store.db.QueryRowContext(ctx, `
		SELECT id, symbol, timeframe, from_time, to_time, started_at, finished_at,
			status, issues_json, error_message
		FROM import_runs
		WHERE id = ?
	`, id).Scan(
		&run.ID,
		&run.Symbol,
		&timeframe,
		&from,
		&to,
		&startedAt,
		&finishedAt,
		&status,
		&run.IssuesJSON,
		&run.ErrorMessage,
	); err != nil {
		return ImportRun{}, fmt.Errorf("query import run %q: %w", id, err)
	}
	run.Timeframe = datasource.Timeframe(timeframe)
	run.From = time.UnixMicro(from).UTC()
	run.To = time.UnixMicro(to).UTC()
	run.StartedAt = time.UnixMicro(startedAt).UTC()
	run.FinishedAt = timePointerFromNullableValue(finishedAt)
	run.Status = ImportRunStatus(status)
	return run, nil
}

// finishImportRun 持久化一个完成或失败的导入状态转换。
func (store *MetaStore) finishImportRun(ctx context.Context, id string, status ImportRunStatus, issuesJSON, errorMessage string) error {
	result, err := store.db.ExecContext(ctx, `
		UPDATE import_runs
		SET status = ?, finished_at = ?, issues_json = ?, error_message = ?
		WHERE id = ?
	`, string(status), time.Now().UTC().Truncate(time.Microsecond).UnixMicro(), issuesJSON, errorMessage, id)
	if err != nil {
		return fmt.Errorf("update import run %q: %w", id, err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check import run %q update result: %w", id, err)
	}
	if updated == 0 {
		return fmt.Errorf("update import run %q: %w", id, sql.ErrNoRows)
	}
	return nil
}

// nullableTimeValue 把零时间转换为 SQLite NULL，其余时间转换为 UTC microseconds。
func nullableTimeValue(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().UnixMicro()
}

// nullableTimePointerValue 把空时间指针转换为 SQLite NULL。
func nullableTimePointerValue(value *time.Time) any {
	if value == nil {
		return nil
	}
	return nullableTimeValue(*value)
}

// timeFromNullableValue 把 SQLite 可空 UTC microseconds 还原为 Go 时间。
func timeFromNullableValue(value sql.NullInt64) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return time.UnixMicro(value.Int64).UTC()
}

// timePointerFromNullableValue 把 SQLite 可空 UTC microseconds 还原为可空时间指针。
func timePointerFromNullableValue(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := time.UnixMicro(value.Int64).UTC()
	return &timestamp
}

// configureAndMigrate 配置本地 SQLite 连接并应用首个元数据 schema migration。
func (store *MetaStore) configureAndMigrate() error {
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := store.db.Exec(statement); err != nil {
			return fmt.Errorf("configure SQLite: %w", err)
		}
	}

	transaction, err := store.db.Begin()
	if err != nil {
		return fmt.Errorf("begin SQLite migration: %w", err)
	}
	defer transaction.Rollback()

	if _, err := transaction.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY
		);
	`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	var applied bool
	if err := transaction.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)",
		metadataSchemaVersion,
	).Scan(&applied); err != nil {
		return fmt.Errorf("query SQLite migration: %w", err)
	}
	if !applied {
		if _, err := transaction.Exec(metadataSchemaV1); err != nil {
			return fmt.Errorf("apply SQLite migration %d: %w", metadataSchemaVersion, err)
		}
		if _, err := transaction.Exec(
			"INSERT INTO schema_migrations(version) VALUES (?)",
			metadataSchemaVersion,
		); err != nil {
			return fmt.Errorf("record SQLite migration %d: %w", metadataSchemaVersion, err)
		}
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit SQLite migration: %w", err)
	}
	return nil
}

const metadataSchemaV1 = `
CREATE TABLE symbols (
	symbol TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	exchange TEXT NOT NULL,
	currency TEXT NOT NULL,
	asset_type TEXT NOT NULL,
	list_date INTEGER,
	delist_date INTEGER,
	timezone TEXT NOT NULL,
	lot_size INTEGER NOT NULL,
	tick_size REAL NOT NULL,
	isin TEXT NOT NULL
);

CREATE TABLE trading_calendar (
	date INTEGER PRIMARY KEY,
	exchange TEXT NOT NULL,
	is_trading_day INTEGER NOT NULL,
	sessions_json TEXT NOT NULL,
	holiday_name TEXT NOT NULL
);

CREATE TABLE corporate_actions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	symbol TEXT NOT NULL,
	action_type TEXT NOT NULL,
	ex_date INTEGER NOT NULL,
	cash_per_share REAL NOT NULL,
	split_ratio REAL NOT NULL,
	source TEXT NOT NULL,
	UNIQUE(symbol, ex_date, action_type)
);

CREATE TABLE import_runs (
	id TEXT PRIMARY KEY,
	symbol TEXT NOT NULL,
	timeframe TEXT NOT NULL,
	from_time INTEGER NOT NULL,
	to_time INTEGER NOT NULL,
	started_at INTEGER NOT NULL,
	finished_at INTEGER,
	status TEXT NOT NULL,
	issues_json TEXT NOT NULL,
	error_message TEXT NOT NULL
);

CREATE INDEX corporate_actions_symbol_ex_date_idx
	ON corporate_actions(symbol, ex_date);
CREATE INDEX import_runs_symbol_timeframe_range_idx
	ON import_runs(symbol, timeframe, from_time, to_time);
`

// TestDBConn 检查固定测试路径上的 SQLite 数据库是否可以建立连接。
func TestDBConn() error {
	db, err := sql.Open("sqlite3", "../data/test.db")
	if err != nil {
		return err
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		return err
	}

	fmt.Println("SQL Connection Success")
	return nil
}
