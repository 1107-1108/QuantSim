package worker

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"
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

func TestSocketCommunication(t *testing.T) {
	socket, err := CreateListener("worker.socket")
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()

	done := make(chan error, 1)

	// Simulate Go Engine
	go func() {
		conn, err := socket.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()

		conn.SetDeadline(time.Now().Add(3 * time.Second))

		// Send request
		err = writeFrame(conn, []byte(`{"method":"on_bar"}`))
		if err != nil {
			done <- err
			return
		}

		// Receive response
		response, err := readFrame(conn)
		if err != nil {
			done <- err
			return
		}

		if string(response) != "orders" {
			done <- fmt.Errorf("unexpected response: %s", response)
			return
		}

		// Engine finished successfully
		done <- nil
	}()

	// Simulate Python Worker
	conn, err := net.Dial("unix", "/tmp/worker.socket")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(3 * time.Second))

	// Receive request
	response, err := readFrame(conn)
	if err != nil {
		t.Fatal(err)
	}

	if string(response) != `{"method":"on_bar"}` {
		t.Fatalf("unexpected request: %s", response)
	}

	// Send orders
	err = writeFrame(conn, []byte("orders"))
	if err != nil {
		t.Fatal(err)
	}

	// Wait for Go Engine
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
