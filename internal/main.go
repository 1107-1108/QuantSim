package main

import (
	"FinanceSimulator/worker"
	"io"
	"log"
	"net/http"
	"net/rpc"
	"net/rpc/jsonrpc"
)

const listenAddr = "127.0.0.1:8001"

// httpConn adapts an HTTP request body / response writer pair to the
// io.ReadWriteCloser interface required by jsonrpc.NewServerCodec.
type httpConn struct {
	in  io.ReadCloser
	out io.Writer
}

func (c *httpConn) Read(p []byte) (int, error)  { return c.in.Read(p) }
func (c *httpConn) Write(p []byte) (int, error) { return c.out.Write(p) }
func (c *httpConn) Close() error                { return c.in.Close() }

func main() {
	if err := rpc.Register(new(worker.Service)); err != nil {
		log.Fatal(err)
	}
	http.HandleFunc("/", rpcHandler)
	log.Printf("JSON-RPC server listening on %s", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, nil))
}

func rpcHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	defer r.Body.Close()
	rpc.ServeCodec(jsonrpc.NewServerCodec(&httpConn{in: r.Body, out: w}))
}
