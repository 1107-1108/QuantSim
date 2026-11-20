package worker

import (
	"bytes"
	"testing"
)

func TestEncodeFrame(t *testing.T) {
	data := []byte(`{"method":"on_bar"}`)

	frame, err := encodeFrame(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) != 23 {
		t.Errorf("frame length = %d, want 23", len(frame))
	}
	result, err := readFrame(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result, data) {
		t.Errorf("payload = %q, want %q", result, data)
	}
}
