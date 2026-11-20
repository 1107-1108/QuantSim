package worker

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestStartEmptyInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		bars []BarMsg
	}{
		{name: "nil"},
		{name: "empty slice", bars: []BarMsg{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := useTestPythonWorker(t, `
class Strategy:
    def on_bar(self, bar):
        raise AssertionError("unexpected bar")
`)
			err := runTestPythonWorker(t, session, tc.bars, onBarTestTimeout)
			if err != nil || len(session.orderList) != 0 {
				t.Fatalf("Start with empty input returned error %v and orders %+v", err, session.orderList)
			}
		})
	}
}

func TestStartSocketAlreadyExists(t *testing.T) {
	file, err := os.OpenFile(onBarTestSocket, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(onBarTestSocket); err != nil {
			t.Error(err)
		}
	})
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	session := &Session{}
	err = session.Start(t.Context(), []BarMsg{onBarTestBar(7)})
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("Start error = %v, want %v", err, os.ErrExist)
	}
	if _, err := os.Stat(onBarTestSocket); err != nil {
		t.Fatalf("Start removed the existing file: %v", err)
	}
}

func TestStartLaunchesPythonAndCollectsOrders(t *testing.T) {
	session := useTestPythonWorker(t, `
class Strategy:
    def on_bar(self, bar):
        if bar["seq"] == 7:
            return [{"symbol": bar["symbol"], "side": "BUY", "type": "MARKET", "qty": 2}]
        if bar["seq"] == 9:
            return [{"symbol": bar["symbol"], "side": "SELL", "type": "LIMIT",
                     "qty": 1, "limit_price": 112.5}]
        return []
`)
	bars := []BarMsg{onBarTestBar(7), onBarTestBar(8), onBarTestBar(9)}
	err := runTestPythonWorker(t, session, bars, onBarTestTimeout)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	want := []OrderMsg{
		{Symbol: "AAPL", Side: "BUY", Type: "MARKET", Qty: 2},
		{Symbol: "AAPL", Side: "SELL", Type: "LIMIT", Qty: 1, LimitPrice: 112.5},
	}
	if !reflect.DeepEqual(session.orderList, want) {
		t.Errorf("orders = %+v, want %+v", session.orderList, want)
	}
}

func TestStartStopsAfterStrategyError(t *testing.T) {
	session := useTestPythonWorker(t, `
class Strategy:
    def on_bar(self, bar):
        if bar["seq"] == 8:
            raise RuntimeError("test strategy error")
        return [{"symbol": bar["symbol"], "side": "BUY", "type": "MARKET", "qty": bar["seq"]}]
`)
	bars := []BarMsg{onBarTestBar(7), onBarTestBar(8), onBarTestBar(9)}
	err := runTestPythonWorker(t, session, bars, onBarTestTimeout)
	assertOnBarError(t, err, "worker error at sequence 8")
	want := []OrderMsg{{Symbol: "AAPL", Side: "BUY", Type: "MARKET", Qty: 7}}
	if !reflect.DeepEqual(session.orderList, want) {
		t.Errorf("orders after strategy failure = %+v, want %+v", session.orderList, want)
	}
}

func TestStartRejectsNonIncreasingBarSequences(t *testing.T) {
	for _, tc := range []struct {
		name string
		seq  uint64
	}{
		{name: "duplicate", seq: 9},
		{name: "decreasing", seq: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := useTestPythonWorker(t, `
class Strategy:
    def on_bar(self, bar):
        with open("seen.txt", "a", encoding="ascii") as file:
            print(bar["seq"], file=file)
        return []
`)
			bars := []BarMsg{onBarTestBar(7), onBarTestBar(9), onBarTestBar(tc.seq), onBarTestBar(10)}
			err := runTestPythonWorker(t, session, bars, onBarTestTimeout)
			assertOnBarError(t, err, "worker error at sequence "+strconv.FormatUint(tc.seq, 10))
			seen, err := os.ReadFile(filepath.Join(filepath.Dir(session.ScriptPath), "seen.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(seen) != "7\n9\n" {
				t.Errorf("strategy saw sequences %q, want %q", seen, "7\n9\n")
			}
		})
	}
}

func TestStartReportsOversizedOrderBatchAsWorkerError(t *testing.T) {
	session := useTestPythonWorker(t, `
class Strategy:
    def on_bar(self, bar):
        order = {"symbol": bar["symbol"], "side": "BUY", "type": "MARKET", "qty": 1}
        return [order] * 350_000  # The encoded batch exceeds 16 MiB.
`)
	err := runTestPythonWorker(t, session, []BarMsg{onBarTestBar(7)}, onBarTestTimeout)
	assertOnBarError(t, err, "worker error at sequence 7")
	if len(session.orderList) != 0 {
		t.Errorf("collected %d orders from an oversized batch", len(session.orderList))
	}
}

func TestStartReturnsPythonExitBeforeConnect(t *testing.T) {
	session := useTestPythonWorker(t, "raise SystemExit(7)")
	err := runTestPythonWorker(t, session, []BarMsg{onBarTestBar(7)}, onBarTestTimeout)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("Start error = %v, want Python exit code 7", err)
	}
}

func TestStartReturnsIfPythonExitsCleanlyBeforeConnect(t *testing.T) {
	session := useTestPythonWorker(t, "raise SystemExit(0)")
	err := runTestPythonWorker(t, session, []BarMsg{onBarTestBar(7)}, onBarTestTimeout)
	assertOnBarError(t, err, "exited before connecting")
}

func TestStartTimesOutWaitingForConnection(t *testing.T) {
	session := useTestPythonWorker(t, "import time\ntime.sleep(30)")
	session.ConnectTimeout = 750 * time.Millisecond
	err := runTestPythonWorker(t, session, []BarMsg{onBarTestBar(7)}, onBarTestTimeout)
	if !isWorkerTimeout(err) {
		t.Fatalf("Start error = %v, want a connection timeout", err)
	}
}

func TestStartTimesOutWhenBarGetsNoReply(t *testing.T) {
	session := useTestPythonWorker(t, `
import time

class Strategy:
    def on_bar(self, bar):
        if bar["seq"] == 8:
            time.sleep(30)
        return [{"symbol": bar["symbol"], "side": "BUY", "type": "MARKET", "qty": 2}]
`)
	bars := []BarMsg{onBarTestBar(7), onBarTestBar(8), onBarTestBar(9)}
	session.BarTimeout = 400 * time.Millisecond
	err := runTestPythonWorker(t, session, bars, onBarTestTimeout)
	if !isWorkerTimeout(err) {
		t.Fatalf("Start error = %v, want a bar timeout", err)
	}
	assertOnBarError(t, err, "8")
	want := []OrderMsg{{Symbol: "AAPL", Side: "BUY", Type: "MARKET", Qty: 2}}
	if !reflect.DeepEqual(session.orderList, want) {
		t.Errorf("orders after bar timeout = %+v, want %+v", session.orderList, want)
	}
}

func TestStartRenewsTimeoutForEachBar(t *testing.T) {
	session := useTestPythonWorker(t, `
import time

class Strategy:
    def on_bar(self, bar):
        time.sleep(0.65)
        return []
`)
	// Each response fits the deadline, but their combined time exceeds it.
	session.BarTimeout = time.Second
	bars := []BarMsg{onBarTestBar(7), onBarTestBar(8)}
	if err := runTestPythonWorker(t, session, bars, onBarTestTimeout); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func useTestPythonWorker(t *testing.T, strategy string) *Session {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for the worker integration test")
	}
	python, err = filepath.Abs(python)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for _, name := range []string{"main.py", "communication.py"} {
		code, err := os.ReadFile(filepath.Join("..", "..", "python-worker", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), code, 0600); err != nil {
			t.Fatal(err)
		}
	}
	const recordPID = `import os
with open("worker.pid", "w", encoding="ascii") as file:
    file.write(str(os.getpid()))

`
	if err := os.WriteFile(filepath.Join(dir, "strategy.py"), []byte(recordPID+strategy), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PYTHONDONTWRITEBYTECODE", "1")
	return &Session{
		PythonPath: python,
		ScriptPath: filepath.Join(dir, "main.py"),
	}
}

func runTestPythonWorker(t *testing.T, session *Session, bars []BarMsg, maxWait time.Duration) error {
	t.Helper()
	if _, err := os.Lstat(onBarTestSocket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("need an unused %s (stat error: %v)", onBarTestSocket, err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), maxWait)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- session.Start(ctx, bars)
	}()

	select {
	case err := <-done:
		if _, statErr := os.Lstat(onBarTestSocket); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("socket was not cleaned up: %v", statErr)
		}
		if len(bars) > 0 {
			assertTestPythonExited(t, session.ScriptPath)
		}
		if ctx.Err() != nil {
			t.Fatalf("Start reached the caller's safety deadline before finishing: %v", err)
		}
		return err
	case <-time.After(maxWait + time.Second):
		t.Fatal("Start did not return after the caller's safety deadline")
		return nil
	}
}

func assertTestPythonExited(t *testing.T, script string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(script), "worker.pid"))
	if err != nil {
		t.Errorf("could not verify worker process cleanup: %v", err)
		return
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		t.Errorf("invalid worker PID %q: %v", data, err)
		return
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("Python process %d still exists after Start (probe error: %v)", pid, err)
	}
}

func isWorkerTimeout(err error) bool {
	var timeout net.Error
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, os.ErrDeadlineExceeded) ||
		(errors.As(err, &timeout) && timeout.Timeout())
}
