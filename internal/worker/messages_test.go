package worker

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func sampleBarMsg() BarMsg {
	return BarMsg{
		Seq:    42,
		Symbol: "AAPL",
		Ts:     1737000000000,
		Open:   100.5,
		High:   101.0,
		Low:    99.5,
		Close:  100.8,
		Volume: 12345.5,
		Account: AccountSnapshot{
			Cash: 50000.25,
			Positions: map[string]PositionSnapshot{
				"AAPL": {Qty: 100, AvgCost: 99.5},
				"MSFT": {Qty: 50, AvgCost: 300.1},
			},
		},
	}
}

func TestBarMsgJSONRoundTrip(t *testing.T) {
	// Arrange
	original := sampleBarMsg()

	// Act
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal BarMsg: %v", err)
	}
	var decoded BarMsg
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal BarMsg: %v", err)
	}

	// Assert
	if !reflect.DeepEqual(original, decoded) {
		t.Errorf("round trip mismatch:\noriginal: %+v\ndecoded:  %+v", original, decoded)
	}
}

// 契约字段名逐键钉死:Python 端按这些键解析,任何 json tag 的重命名都会让 worker 静默挂掉。
func TestBarMsgJSONUsesContractKeys(t *testing.T) {
	// Arrange
	msg := sampleBarMsg()

	// Act
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal BarMsg: %v", err)
	}

	// Assert: 顶层键
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal to wire map: %v", err)
	}
	wantKeys := []string{"seq", "symbol", "ts", "o", "h", "l", "c", "v", "account"}
	gotKeys := make([]string, 0, len(wire))
	for key := range wire {
		gotKeys = append(gotKeys, key)
	}
	sort.Strings(gotKeys)
	sort.Strings(wantKeys)
	if !reflect.DeepEqual(wantKeys, gotKeys) {
		t.Errorf("top-level wire keys mismatch: want %v, got %v", wantKeys, gotKeys)
	}

	// Assert: account 内部键
	var account map[string]json.RawMessage
	if err := json.Unmarshal(wire["account"], &account); err != nil {
		t.Fatalf("unmarshal account: %v", err)
	}
	for _, key := range []string{"cash", "positions"} {
		if _, ok := account[key]; !ok {
			t.Errorf("account missing contract key %q", key)
		}
	}

	// Assert: position 内部键
	var positions map[string]map[string]json.RawMessage
	if err := json.Unmarshal(account["positions"], &positions); err != nil {
		t.Fatalf("unmarshal positions: %v", err)
	}
	for _, key := range []string{"qty", "avg_cost"} {
		if _, ok := positions["AAPL"][key]; !ok {
			t.Errorf("position missing contract key %q", key)
		}
	}
}

// Python 端按 dict 解析 positions;nil map 会序列化成 null,worker 解析直接炸。
// 构造入口必须保证空持仓是 {} 而不是 null。
func TestNewAccountSnapshotEmptyPositionsIsObjectNotNull(t *testing.T) {
	// Arrange
	snapshot := NewAccountSnapshot(10000)

	// Act
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal AccountSnapshot: %v", err)
	}

	// Assert
	if !bytes.Contains(data, []byte(`"positions":{}`)) {
		t.Errorf("empty positions must serialize as {} not null, got %s", data)
	}
}
