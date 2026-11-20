package worker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

const onBarTestSocket = "/tmp/worker.socket"
const onBarTestTimeout = 3 * time.Second

func TestOnBarOK(t *testing.T) {
	const seq = uint64(1<<64 - 1)
	peer, wait := startOnBarTest(t, t.Context(), onBarTestBar(seq))
	assertOnBarRequest(t, peer, seq)
	sendOnBarBytes(t, peer, onBarTestFrame([]byte(fmt.Sprintf(`{"seq":%d,"condition":"OK"}`, seq))))

	orders, err := wait()
	if err != nil {
		t.Fatalf("OnBar: %v", err)
	}
	assertNoNextBar(t, peer)
	if len(orders) != 0 {
		t.Errorf("OK produced orders: %+v", orders)
	}
}

func TestOnBarReturnsOrders(t *testing.T) {
	peer, wait := startOnBarTest(t, t.Context(), onBarTestBar(7))
	assertOnBarRequest(t, peer, 7)
	sendOnBarBytes(t, peer, onBarTestFrame([]byte(`{"seq":7,"condition":"ORDER","orderBatch":[{"symbol":"AAPL","side":"BUY","type":"MARKET","qty":2},{"symbol":"MSFT","side":"SELL","type":"LIMIT","qty":3,"limit_price":112.5}]}`)))

	orders, err := wait()
	if err != nil {
		t.Fatalf("OnBar: %v", err)
	}
	want := []OrderMsg{
		{Symbol: "AAPL", Side: "BUY", Type: "MARKET", Qty: 2},
		{Symbol: "MSFT", Side: "SELL", Type: "LIMIT", Qty: 3, LimitPrice: 112.5},
	}
	if !reflect.DeepEqual(orders, want) {
		t.Errorf("orders = %+v, want %+v", orders, want)
	}
}

func TestOnBarWaitsForCompleteResponseFrame(t *testing.T) {
	peer, wait := startOnBarTest(t, t.Context(), onBarTestBar(7))
	assertOnBarRequest(t, peer, 7)
	frame := onBarTestFrame([]byte(`{"seq":7,"condition":"OK"}`))
	sendOnBarBytes(t, peer, frame[:2])
	sendOnBarBytes(t, peer, frame[2:6])
	sendOnBarBytes(t, peer, frame[6:])

	if _, err := wait(); err != nil {
		t.Fatalf("OnBar: %v", err)
	}
}

func TestOnBarRejectsNilConnection(t *testing.T) {
	session := &Session{}
	orders, err := session.OnBar(t.Context(), nil, onBarTestBar(7))
	assertOnBarError(t, err, "worker connection is nil")
	if len(orders) != 0 {
		t.Errorf("nil connection produced orders: %+v", orders)
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
			_, wait := startOnBarTest(t, t.Context(), bar)
			orders, err := wait()
			if tc.want != "" {
				assertOnBarError(t, err, tc.want)
			} else {
				var unsupported *json.UnsupportedValueError
				if !errors.As(err, &unsupported) {
					t.Fatalf("OnBar error = %v, want *json.UnsupportedValueError", err)
				}
			}
			if len(orders) != 0 {
				t.Errorf("invalid input produced orders: %+v", orders)
			}
		})
	}
}

func TestOnBarRejectsOversizedRequest(t *testing.T) {
	bar := onBarTestBar(7)
	bar.Symbol = strings.Repeat("A", 16<<20)
	_, wait := startOnBarTest(t, t.Context(), bar)
	_, err := wait()
	assertOnBarError(t, err, "exceeds")
}

func TestOnBarReturnsWriteError(t *testing.T) {
	peer, wait := startOnBarTest(t, t.Context(), onBarTestBar(7))
	var header [4]byte
	if _, err := io.ReadFull(peer, header[:]); err != nil {
		t.Fatalf("read request header: %v", err)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("close peer: %v", err)
	}
	_, err := wait()
	assertOnBarError(t, err, "write frame")
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
			peer, wait := startOnBarTest(t, t.Context(), onBarTestBar(7))
			assertOnBarRequest(t, peer, 7)
			if len(tc.frame) > 0 {
				sendOnBarBytes(t, peer, tc.frame)
			}
			if err := peer.Close(); err != nil {
				t.Fatalf("close response stream: %v", err)
			}

			orders, err := wait()
			assertOnBarError(t, err, tc.want)
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Errorf("OnBar error = %v, want wrapped %v", err, tc.cause)
			}
			if len(orders) != 0 {
				t.Errorf("invalid frame produced orders: %+v", orders)
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
			peer, wait := startOnBarTest(t, t.Context(), onBarTestBar(7))
			assertOnBarRequest(t, peer, 7)
			sendOnBarBytes(t, peer, onBarTestFrame([]byte(tc.payload)))
			orders, err := wait()
			assertOnBarError(t, err, tc.want)
			if len(orders) != 0 {
				t.Errorf("invalid response produced orders: %+v", orders)
			}
		})
	}
}

func TestOnBarHonorsContext(t *testing.T) {
	for _, tc := range []struct {
		name       string
		readBar    bool
		cancelRead bool
	}{
		{name: "blocked write"},
		{name: "no response", readBar: true},
		{name: "canceled while waiting for response", readBar: true, cancelRead: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			if tc.cancelRead {
				ctx, cancel = context.WithCancel(t.Context())
			} else {
				ctx, cancel = context.WithTimeout(t.Context(), 250*time.Millisecond)
			}
			defer cancel()

			peer, wait := startOnBarTest(t, ctx, onBarTestBar(7))
			if tc.readBar {
				assertOnBarRequest(t, peer, 7)
			}
			if tc.cancelRead {
				cancel()
			}

			orders, err := wait()
			if tc.cancelRead {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("OnBar error = %v, want context.Canceled", err)
				}
			} else if !isWorkerTimeout(err) {
				t.Errorf("OnBar error = %v, want a timeout", err)
			}
			if len(orders) != 0 {
				t.Errorf("OnBar returned orders without a response: %+v", orders)
			}
		})
	}
}

func startOnBarTest(t *testing.T, ctx context.Context, bar BarMsg) (net.Conn, func() ([]OrderMsg, error)) {
	t.Helper()
	conn, peer := net.Pipe()
	t.Cleanup(func() {
		conn.Close()
		peer.Close()
	})
	if err := peer.SetDeadline(time.Now().Add(onBarTestTimeout)); err != nil {
		t.Fatal(err)
	}

	type result struct {
		orders []OrderMsg
		err    error
	}

	session := &Session{}
	done := make(chan result, 1)
	go func() {
		orders, err := session.OnBar(ctx, conn, bar)
		done <- result{orders: orders, err: err}
	}()
	wait := func() ([]OrderMsg, error) {
		t.Helper()
		select {
		case result := <-done:
			return result.orders, result.err
		case <-time.After(onBarTestTimeout):
			t.Fatal("OnBar did not return")
			return nil, nil
		}
	}
	return peer, wait
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

func assertNoNextBar(t *testing.T, peer net.Conn) {
	t.Helper()
	if err := peer.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var next [1]byte
	if n, err := peer.Read(next[:]); n != 0 || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected no additional frame, read %d bytes with %v", n, err)
	}
	if err := peer.SetReadDeadline(time.Now().Add(onBarTestTimeout)); err != nil {
		t.Fatalf("reset read deadline: %v", err)
	}
}

func assertOnBarError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("OnBar error = %v, want %q", err, want)
	}
}
