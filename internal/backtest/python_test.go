package backtest_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"FinanceSimulator/backtest"
	"FinanceSimulator/datasource"
	"FinanceSimulator/portfolio"
	"FinanceSimulator/worker"
)

func TestRunPythonSharesAccountAcrossSymbols(t *testing.T) {
	session := pythonSession(t, `
class Strategy:
    def on_bar(self, bar):
        seq = bar["seq"]
        assert bar["symbol"] == ("AAPL" if seq in (1, 3) else "MSFT"), bar
        if seq == 1:
            self.first_ts = bar["ts"]
        if seq <= 2:
            assert bar["ts"] == self.first_ts, bar
            assert bar["account"] == {"cash": 1000, "positions": {}}, bar
            return [{"symbol": bar["symbol"], "side": "BUY", "type": "MARKET", "qty": 1}]

        assert seq in (3, 4) and bar["ts"] - self.first_ts == 60_000, bar
        if seq == 3:
            assert (bar["o"], bar["h"], bar["l"], bar["c"], bar["v"]) == (110, 112, 110, 112, 30), bar
        assert bar["account"] == {"cash": 690, "positions": {
            "AAPL": {"qty": 1, "avg_cost": 110}, "MSFT": {"qty": 1, "avg_cost": 200}
        }}, bar
        return []
`)
	first := time.Date(2024, 4, 2, 14, 30, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	bars := []backtest.SymbolBar{
		quote("AAPL", first, 99, 100), quote("MSFT", first, 189, 190),
		quote("AAPL", second, 110, 112), quote("MSFT", second, 200, 201),
	}
	bars[2].Bar.Volume = 30
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	result, err := backtest.RunPython(ctx, initialAccount(t, 1000), bars, session)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Fills) != 2 || result.Fills[0].Price != 110 || result.Fills[1].Price != 200 ||
		!result.Fills[0].Time.Equal(second) || !result.Fills[1].Time.Equal(second) {
		t.Errorf("fills = %+v, want both orders filled at the second timestamp", result.Fills)
	}
	if len(result.Equity) != 2 || result.Equity[1].Value != 1003 || len(result.UnfilledOrders) != 0 {
		t.Errorf("equity = %+v, unfilled orders = %+v", result.Equity, result.UnfilledOrders)
	}
}

func TestRunPythonSeesSplitAdjustedPosition(t *testing.T) {
	session := pythonSession(t, `
class Strategy:
    def on_bar(self, bar):
        assert bar["account"]["cash"] == 600, bar
        expected = {"qty": 1, "avg_cost": 400} if bar["seq"] == 1 else {"qty": 4, "avg_cost": 100}
        assert bar["account"]["positions"]["AAPL"] == expected, bar
        return []
`)
	friday := time.Date(2020, 8, 28, 13, 30, 0, 0, time.UTC)
	monday := friday.AddDate(0, 0, 3)
	account, _, err := portfolio.ApplyFill(initialAccount(t, 1000), portfolio.Fill{
		Seq: 1, Symbol: "AAPL", Side: portfolio.SideBuy, Qty: 1, Price: 400, Time: friday.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	bars := []backtest.SymbolBar{quote("AAPL", friday, 400, 400), quote("AAPL", monday, 100, 100)}
	split := datasource.CorporateAction{Symbol: "AAPL", Type: datasource.ActionSplit, Date: monday, SplitRatio: 4}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	result, err := backtest.RunPython(ctx, account, bars, session, split)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Equity) != 2 || result.Equity[0].Value != 1000 || result.Equity[1].Value != 1000 {
		t.Errorf("want unchanged equity across the split, got %+v", result.Equity)
	}
}

func TestRunPythonReturnsStrategyFailure(t *testing.T) {
	session := pythonSession(t, `
class Strategy:
    def on_bar(self, bar):
        raise RuntimeError("failed on purpose")
`)
	bar := quote("AAPL", time.Date(2024, 4, 2, 14, 30, 0, 0, time.UTC), 100, 100)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := backtest.RunPython(ctx, initialAccount(t, 1000), []backtest.SymbolBar{bar}, session); err == nil ||
		!strings.Contains(err.Error(), "worker error at sequence 1") {
		t.Fatalf("got %v, want a strategy failure for sequence 1", err)
	}
}

func pythonSession(t *testing.T, strategy string) *worker.Session {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for the backtest integration test")
	}
	dir := t.TempDir()
	for _, name := range []string{"main.py", "communication.py"} {
		code, err := os.ReadFile(filepath.Join("..", "..", "python-worker", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), code, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "strategy.py"), []byte(strategy), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PYTHONDONTWRITEBYTECODE", "1")
	t.Setenv("PYTHONOPTIMIZE", "0")
	return &worker.Session{PythonPath: python, ScriptPath: filepath.Join(dir, "main.py")}
}
