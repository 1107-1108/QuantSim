package worker

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

const onBarTestSocket = "/tmp/worker.socket"
const onBarTestTimeout = 3 * time.Second

func TestOnBarEmptyInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		bars []BarMsg
	}{
		{name: "nil"},
		{name: "empty slice", bars: []BarMsg{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, peer, wait := startOnBarTest(t, tc.bars)
			if err := wait(); err != nil {
				t.Fatalf("OnBar: %v", err)
			}
			assertOnBarPeerClosed(t, peer)
			if len(session.orderList) != 0 {
				t.Errorf("empty input produced orders: %+v", session.orderList)
			}
		})
	}
}

func TestOnBarOK(t *testing.T) {
	const seq = uint64(1<<64 - 1)
	session, peer, wait := startOnBarTest(t, []BarMsg{onBarTestBar(seq)})
	assertOnBarRequest(t, peer, seq)
	sendOnBarBytes(t, peer, onBarTestFrame([]byte(fmt.Sprintf(`{"seq":%d,"condition":"OK"}`, seq))))

	if err := wait(); err != nil {
		t.Fatalf("OnBar: %v", err)
	}
	assertOnBarPeerClosed(t, peer)
	if len(session.orderList) != 0 {
		t.Errorf("OK produced orders: %+v", session.orderList)
	}
}

func TestOnBarReturnsListenerError(t *testing.T) {
	// A dangling link makes the path unavailable for bind without triggering
	// the existing-file check in CreateListener.
	target := filepath.Join(t.TempDir(), "missing-dir", "worker.sock")
	if err := os.Symlink(target, onBarTestSocket); err != nil {
		t.Fatalf("reserve the socket path for this test: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(onBarTestSocket); err != nil {
			t.Errorf("remove test symlink: %v", err)
		}
	})

	session := &Session{}
	err := session.OnBar([]BarMsg{onBarTestBar(7)})
	if !errors.Is(err, syscall.EADDRINUSE) && !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("OnBar error = %v, want socket bind failure", err)
	}
}

func TestOnBarWaitsForCompleteResponseFrame(t *testing.T) {
	_, peer, wait := startOnBarTest(t, []BarMsg{onBarTestBar(7), onBarTestBar(8)})
	assertOnBarRequest(t, peer, 7)
	frame := onBarTestFrame([]byte(`{"seq":7,"condition":"OK"}`))
	sendOnBarBytes(t, peer, frame[:2])
	assertOnBarWaitsForResponse(t, peer)
	sendOnBarBytes(t, peer, frame[2:6])
	assertOnBarWaitsForResponse(t, peer)
	sendOnBarBytes(t, peer, frame[6:])

	assertOnBarRequest(t, peer, 8)
	sendOnBarBytes(t, peer, onBarTestFrame([]byte(`{"seq":8,"condition":"OK"}`)))
	if err := wait(); err != nil {
		t.Fatalf("OnBar: %v", err)
	}
	assertOnBarPeerClosed(t, peer)
}

func TestOnBarWaitsForResponsesAndAccumulatesOrders(t *testing.T) {
	session, peer, wait := startOnBarTest(t, []BarMsg{onBarTestBar(7), onBarTestBar(8), onBarTestBar(9)})
	assertOnBarRequest(t, peer, 7)
	assertOnBarWaitsForResponse(t, peer)
	sendOnBarBytes(t, peer, onBarTestFrame([]byte(`{"seq":7,"condition":"ORDER","orderBatch":[{"symbol":"AAPL","side":"BUY","type":"MARKET","qty":2}]}`)))

	assertOnBarRequest(t, peer, 8)
	sendOnBarBytes(t, peer, onBarTestFrame([]byte(`{"seq":8,"condition":"OK"}`)))

	assertOnBarRequest(t, peer, 9)
	sendOnBarBytes(t, peer, onBarTestFrame([]byte(`{"seq":9,"condition":"ORDER","orderBatch":[{"symbol":"AAPL","side":"SELL","type":"LIMIT","qty":1,"limit_price":112.5},{"symbol":"MSFT","side":"BUY","type":"MARKET","qty":3}]}`)))

	if err := wait(); err != nil {
		t.Fatalf("OnBar: %v", err)
	}
	assertOnBarPeerClosed(t, peer)
	want := []OrderMsg{
		{Symbol: "AAPL", Side: "BUY", Type: "MARKET", Qty: 2},
		{Symbol: "AAPL", Side: "SELL", Type: "LIMIT", Qty: 1, LimitPrice: 112.5},
		{Symbol: "MSFT", Side: "BUY", Type: "MARKET", Qty: 3},
	}
	if !reflect.DeepEqual(session.orderList, want) {
		t.Errorf("orders = %+v, want %+v", session.orderList, want)
	}
}

func TestOnBarRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name       string
		invalidate func(*BarMsg)
		want       string
	}{
		{name: "zero sequence", invalidate: func(bar *BarMsg) { bar.Seq = 0 }, want: "sequence number cannot be 0"},
		{name: "NaN price", invalidate: func(bar *BarMsg) { bar.Close = math.NaN() }},
		{name: "infinite cash", invalidate: func(bar *BarMsg) { bar.Account.Cash = math.Inf(1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bar := onBarTestBar(7)
			tc.invalidate(&bar)
			session, peer, wait := startOnBarTest(t, []BarMsg{bar, onBarTestBar(8)})
			if err := wait(); tc.want != "" {
				assertOnBarError(t, err, tc.want)
			} else {
				var unsupported *json.UnsupportedValueError
				if !errors.As(err, &unsupported) {
					t.Fatalf("OnBar error = %v, want *json.UnsupportedValueError", err)
				}
			}
			assertOnBarPeerClosed(t, peer)
			if len(session.orderList) != 0 {
				t.Errorf("invalid input produced orders: %+v", session.orderList)
			}
		})
	}
}

func TestOnBarRejectsOversizedRequest(t *testing.T) {
	bar := onBarTestBar(7)
	bar.Symbol = strings.Repeat("A", 16<<20)
	_, peer, wait := startOnBarTest(t, []BarMsg{bar})
	assertOnBarError(t, wait(), "exceeds")
	assertOnBarPeerClosed(t, peer)
}

func TestOnBarReturnsWriteError(t *testing.T) {
	bar := onBarTestBar(7)
	// A large request keeps the write pending when the peer closes after the header.
	bar.Symbol = strings.Repeat("A", 8<<20)
	_, peer, wait := startOnBarTest(t, []BarMsg{bar})
	var header [4]byte
	if _, err := io.ReadFull(peer, header[:]); err != nil {
		t.Fatalf("read request header: %v", err)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("close peer: %v", err)
	}
	assertOnBarError(t, wait(), "write frame")
}

func TestOnBarRejectsInvalidResponseFrames(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame []byte
		want  string
		cause error
	}{
		{name: "EOF before header", want: "read frame header", cause: io.EOF},
		{name: "truncated header", frame: []byte{0, 0}, want: "read frame header", cause: io.ErrUnexpectedEOF},
		{name: "truncated payload", frame: []byte{0, 0, 0, 4, '{'}, want: "read frame payload", cause: io.ErrUnexpectedEOF},
		{name: "zero length", frame: []byte{0, 0, 0, 0}, want: "invalid frame length"},
		{name: "oversized length", frame: []byte{1, 0, 0, 1}, want: "invalid frame length"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, peer, wait := startOnBarTest(t, []BarMsg{onBarTestBar(7), onBarTestBar(8)})
			assertOnBarRequest(t, peer, 7)
			if len(tc.frame) > 0 {
				sendOnBarBytes(t, peer, tc.frame)
			}
			if err := peer.CloseWrite(); err != nil {
				t.Fatalf("close response stream: %v", err)
			}

			err := wait()
			assertOnBarError(t, err, tc.want)
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Errorf("OnBar error = %v, want wrapped %v", err, tc.cause)
			}
			assertOnBarPeerClosed(t, peer)
			if len(session.orderList) != 0 {
				t.Errorf("invalid frame produced orders: %+v", session.orderList)
			}
		})
	}
}

func TestOnBarRejectsInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
		want    string
	}{
		{name: "malformed JSON", payload: `{"seq":`, want: "unexpected end of JSON"},
		{name: "response is array", payload: "[]", want: "cannot unmarshal array"},
		{name: "sequence type", payload: `{"seq":"7","condition":"OK"}`, want: "cannot unmarshal string"},
		{name: "order batch type", payload: `{"seq":7,"condition":"ORDER","orderBatch":{}}`, want: "cannot unmarshal object"},
		{name: "missing sequence", payload: `{"condition":"OK"}`, want: "sequence mismatch: expected 7, got 0"},
		{name: "incorrect sequence", payload: `{"seq":8,"condition":"ORDER","orderBatch":[{"symbol":"AAPL","qty":2}]}`, want: "sequence mismatch: expected 7, got 8"},
		{name: "missing condition", payload: `{"seq":7}`, want: "unknown worker condition"},
		{name: "unknown condition", payload: `{"seq":7,"condition":"UNKNOWN"}`, want: "unknown worker condition: UNKNOWN"},
		{name: "worker error", payload: `{"seq":7,"condition":"ERROR"}`, want: "worker error at sequence 7"},
		{name: "missing orders", payload: `{"seq":7,"condition":"ORDER"}`, want: "ORDER response contains no orders"},
		{name: "null orders", payload: `{"seq":7,"condition":"ORDER","orderBatch":null}`, want: "ORDER response contains no orders"},
		{name: "empty orders", payload: `{"seq":7,"condition":"ORDER","orderBatch":[]}`, want: "ORDER response contains no orders"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, peer, wait := startOnBarTest(t, []BarMsg{onBarTestBar(7), onBarTestBar(8)})
			assertOnBarRequest(t, peer, 7)
			sendOnBarBytes(t, peer, onBarTestFrame([]byte(tc.payload)))
			assertOnBarError(t, wait(), tc.want)
			assertOnBarPeerClosed(t, peer)
			if len(session.orderList) != 0 {
				t.Errorf("invalid response produced orders: %+v", session.orderList)
			}
		})
	}
}

func TestOnBarStopsAfterLaterError(t *testing.T) {
	session, peer, wait := startOnBarTest(t, []BarMsg{onBarTestBar(7), onBarTestBar(8), onBarTestBar(9)})
	assertOnBarRequest(t, peer, 7)
	sendOnBarBytes(t, peer, onBarTestFrame([]byte(`{"seq":7,"condition":"ORDER","orderBatch":[{"symbol":"AAPL","side":"BUY","type":"MARKET","qty":2}]}`)))
	assertOnBarRequest(t, peer, 8)
	sendOnBarBytes(t, peer, onBarTestFrame([]byte(`{"seq":8,"condition":"ERROR"}`)))

	assertOnBarError(t, wait(), "worker error at sequence 8")
	assertOnBarPeerClosed(t, peer)
	want := []OrderMsg{{Symbol: "AAPL", Side: "BUY", Type: "MARKET", Qty: 2}}
	if !reflect.DeepEqual(session.orderList, want) {
		t.Errorf("orders after later error = %+v, want %+v", session.orderList, want)
	}
}

func startOnBarTest(t *testing.T, bars []BarMsg) (*Session, *net.UnixConn, func() error) {
	t.Helper()
	// OnBar binds a fixed socket path. Keep these tests serial and preserve any existing socket.
	if _, err := os.Lstat(onBarTestSocket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("need an unused %s (stat error: %v)", onBarTestSocket, err)
	}

	session := &Session{}
	finished := make(chan struct{})
	var runErr error
	go func() {
		runErr = session.OnBar(bars)
		close(finished)
	}()

	deadline := time.NewTimer(onBarTestTimeout)
	defer deadline.Stop()
	retry := time.NewTicker(5 * time.Millisecond)
	defer retry.Stop()
	addr := &net.UnixAddr{Name: onBarTestSocket, Net: "unix"}
	var peer *net.UnixConn
	for {
		conn, err := net.DialUnix("unix", nil, addr)
		if err == nil {
			peer = conn
			break
		}
		select {
		case <-finished:
			t.Fatalf("OnBar exited before worker connected: %v", runErr)
		case <-deadline.C:
			t.Fatalf("timed out connecting the test worker: %v", err)
		case <-retry.C:
		}
	}
	wait := func() error {
		t.Helper()
		select {
		case <-finished:
			return runErr
		case <-time.After(onBarTestTimeout):
			t.Fatal("OnBar did not return")
			return nil
		}
	}
	t.Cleanup(func() {
		_ = peer.Close()
		select {
		case <-finished:
			if _, err := os.Lstat(onBarTestSocket); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("listener socket still present after OnBar (stat error: %v)", err)
			}
		case <-time.After(onBarTestTimeout):
			t.Error("OnBar did not return after disconnecting the test worker")
		}
	})
	if err := peer.SetDeadline(time.Now().Add(onBarTestTimeout)); err != nil {
		t.Fatalf("set worker deadline: %v", err)
	}
	return session, peer, wait
}

func onBarTestBar(seq uint64) BarMsg {
	return BarMsg{
		Seq: seq, Symbol: "AAPL", Ts: 1700000000000,
		Open: 100, High: 110, Low: 99.5, Close: 105.25, Volume: 1234,
		Account: AccountSnapshot{
			Cash:      10000,
			Positions: map[string]PositionSnapshot{"AAPL": {Qty: 2, AvgCost: 98.5}},
		},
	}
}

func assertOnBarRequest(t *testing.T, peer io.Reader, seq uint64) {
	t.Helper()
	var header [4]byte
	if _, err := io.ReadFull(peer, header[:]); err != nil {
		t.Fatalf("read bar %d header: %v", seq, err)
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > 4096 {
		t.Fatalf("bar %d: unexpected request frame length %d", seq, length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(peer, payload); err != nil {
		t.Fatalf("read bar %d payload: %v", seq, err)
	}
	want := fmt.Sprintf(`{"seq":%d,"symbol":"AAPL","ts":1700000000000,"o":100,"h":110,"l":99.5,"c":105.25,"v":1234,"account":{"cash":10000,"positions":{"AAPL":{"qty":2,"avg_cost":98.5}}}}`, seq)
	decode := func(raw []byte) map[string]any {
		t.Helper()
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var fields map[string]any
		if err := d.Decode(&fields); err != nil {
			t.Fatalf("decode bar payload %s: %v", raw, err)
		}
		if err := d.Decode(new(any)); err != io.EOF {
			t.Fatalf("bar frame has trailing data: %s", raw)
		}
		return fields
	}
	if !reflect.DeepEqual(decode(payload), decode([]byte(want))) {
		t.Fatalf("bar payload = %s, want %s", payload, want)
	}
}

func onBarTestFrame(payload []byte) []byte {
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	return frame
}

func sendOnBarBytes(t *testing.T, peer io.Writer, frame []byte) {
	t.Helper()
	if n, err := peer.Write(frame); err != nil || n != len(frame) {
		t.Fatalf("write worker response: %d/%d bytes, %v", n, len(frame), err)
	}
}

func assertOnBarWaitsForResponse(t *testing.T, peer *net.UnixConn) {
	t.Helper()
	if err := peer.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var next [1]byte
	if n, err := peer.Read(next[:]); n != 0 || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected OnBar to wait for a response, read %d bytes with %v", n, err)
	}
	if err := peer.SetReadDeadline(time.Now().Add(onBarTestTimeout)); err != nil {
		t.Fatalf("reset read deadline: %v", err)
	}
}

func assertOnBarPeerClosed(t *testing.T, peer io.Reader) {
	t.Helper()
	var unexpected [1]byte
	if n, err := peer.Read(unexpected[:]); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("expected connection to close without sending another bar, read %d bytes with %v", n, err)
	}
}

func assertOnBarError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("OnBar error = %v, want %q", err, want)
	}
}
