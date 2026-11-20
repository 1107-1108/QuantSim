package worker

import (
	"bytes"
	"fmt"
	"testing"
)

func TestWriteFrame(t *testing.T) {
	var buf bytes.Buffer
	data := []byte(`{"method":"on_bar"}`)

	err := writeFrame(&buf, data)
	if err != nil {
		t.Error(err)
	}
	fmt.Println("Buffer length:", buf.Len())
	if buf.Len() != 23 {
		t.Error("Buffer length error")
	}
	result, err := readFrame(&buf)
	if err != nil {
		t.Error(err)
	}
	fmt.Println("Received", string(result))
	if string(result) != string(data) {
		t.Error("Data not the same!")
	}
}
