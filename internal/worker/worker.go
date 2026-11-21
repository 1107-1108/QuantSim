package worker

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"
)

// maxFrameSize 是单帧上限 16 MiB
// 帧格式:4 字节无符号大端长度头 + length 字节 UTF-8 JSON 负载。
const maxFrameSize = 16 << 20
const defaultConnectTimeout = 5 * time.Second
const defaultBarTimeout = 5 * time.Second
const workerExitTimeout = time.Second

// Session 与每个Worker的通讯
type Session struct {
	PythonPath     string        // 为空时使用 python3
	ScriptPath     string        // 为空时向上查找 python-worker/main.py
	ConnectTimeout time.Duration // 0 使用默认值
	BarTimeout     time.Duration // 0 使用默认值

	socket    net.Listener
	orderList []OrderMsg
	running   atomic.Bool
}

type WorkerResponse struct {
	Seq        uint64     `json:"seq"`
	Condition  string     `json:"condition"`
	OrderBatch []OrderMsg `json:"orderBatch"`
}

// CreateListener 创建一个Unix Socket
// sockName 文件名
func (s *Session) CreateListener(sockName string) error {
	sPath := fmt.Sprintf("/tmp/%s", sockName)
	if _, err := os.Lstat(sPath); err == nil {
		return fmt.Errorf("socket %s already exists: %w", sPath, os.ErrExist)
	}
	socket, err := net.Listen("unix", sPath)
	if err != nil {
		return err
	}
	s.socket = socket
	log.Println("Socket created successfully")
	return nil
}

// Start 启动 Python worker，依次发送预先准备好的行情消息并收集订单。
func (s *Session) Start(ctx context.Context, msgs []BarMsg) error {
	return s.WithWorker(ctx, func(sendBar SendBar) error {
		for _, msg := range msgs {
			orders, err := sendBar(msg)
			if err != nil {
				return err
			}
			s.orderList = append(s.orderList, orders...)
		}
		return nil
	})
}

// SendBar 同步发送一根 bar，等待 Python 返回本根策略订单。
type SendBar func(BarMsg) ([]OrderMsg, error)

// WithWorker 启动一个 Python worker，在连接建立后调用 run，并在退出时回收进程和连接。
// run 必须在返回前串行调用 sendBar。同一 Session 一次只能运行一个 worker。
func (s *Session) WithWorker(ctx context.Context, run func(sendBar SendBar) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if run == nil {
		return fmt.Errorf("worker session requires a callback")
	}
	if !s.running.CompareAndSwap(false, true) {
		return fmt.Errorf("worker session is already running")
	}
	defer s.running.Store(false)

	python := s.PythonPath
	if python == "" {
		python = "python3"
	}
	script, err := workerScriptPath(s.ScriptPath)
	if err != nil {
		return err
	}
	connectTimeout := s.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = defaultConnectTimeout
	}
	barTimeout := s.BarTimeout
	if barTimeout <= 0 {
		barTimeout = defaultBarTimeout
	}

	// 使用短路径，避免较长的 TMPDIR 超出 Unix Socket 的路径长度上限。
	socketDir, err := os.MkdirTemp("/tmp", "finance-worker-")
	if err != nil {
		return fmt.Errorf("create worker socket directory: %w", err)
	}
	defer os.RemoveAll(socketDir)
	listener, err := net.Listen("unix", filepath.Join(socketDir, "worker.sock"))
	if err != nil {
		return fmt.Errorf("listen for Python worker: %w", err)
	}
	defer listener.Close()

	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	cmd := exec.CommandContext(workerCtx, python, script, "--socket", listener.Addr().String())
	cmd.Dir = filepath.Dir(script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Python worker: %w", err)
	}

	workerDone := make(chan struct{})
	var workerErr error
	go func() {
		workerErr = cmd.Wait()
		close(workerDone)
		// Python 提前退出时，让仍在等待连接的 Accept 返回。
		listener.Close()
	}()
	defer func() {
		stopWorker()
		<-workerDone
	}()

	connectCtx, cancelConnect := context.WithTimeout(ctx, connectTimeout)
	stopAccept := context.AfterFunc(connectCtx, func() { listener.Close() })
	conn, err := listener.Accept()
	stopAccept()
	connectErr := connectCtx.Err()
	cancelConnect()
	if err != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if connectErr != nil {
			return fmt.Errorf("wait for Python worker connection: %w", connectErr)
		}
		select {
		case <-workerDone:
			if workerErr != nil {
				return fmt.Errorf("Python worker exited before connecting: %w", workerErr)
			}
			return fmt.Errorf("Python worker exited before connecting")
		default:
			return fmt.Errorf("accept Python worker connection: %w", err)
		}
	}
	defer conn.Close()

	// 回测循环可以等撮合完成后，再用实时生成的账户快照发出这一帧。
	sendBar := func(msg BarMsg) ([]OrderMsg, error) {
		barCtx, cancelBar := context.WithTimeout(ctx, barTimeout)
		defer cancelBar()
		orders, err := s.OnBar(barCtx, conn, msg)
		if err != nil {
			return nil, fmt.Errorf("bar seq %d: %w", msg.Seq, err)
		}
		return orders, nil
	}
	if err := run(sendBar); err != nil {
		return err
	}

	// 关闭连接后，Python 收到 EOF 并退出。
	conn.Close()
	timer := time.NewTimer(workerExitTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-workerDone:
		if workerErr != nil {
			return fmt.Errorf("wait for Python worker exit: %w", workerErr)
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("wait for Python worker exit: %w", context.DeadlineExceeded)
	}
}

func workerScriptPath(script string) (string, error) {
	if script != "" {
		return filepath.Abs(script)
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		path := filepath.Join(dir, "python-worker", "main.py")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("python-worker/main.py not found; set Session.ScriptPath")
		}
		dir = parent
	}
}

// OnBar 往 Python 推送一根 Bar，并返回这一帧产生的订单。同一连接上的调用须串行执行。
// 开始发送后的读写或协议错误会关闭连接。发送前的校验错误和完整的 ERROR 响应会保留连接。
func (s *Session) OnBar(ctx context.Context, conn net.Conn, msg BarMsg) (orders []OrderMsg, err error) {
	if conn == nil {
		return nil, fmt.Errorf("worker connection is nil")
	}
	if msg.Seq == 0 {
		return nil, fmt.Errorf("sequence number cannot be 0")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	frame, err := encodeFrame(data)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		conn.SetDeadline(time.Now())
		close(interrupted)
	})
	reusable := false
	defer func() {
		if !stop() {
			<-interrupted
			hasDeadline = true
		}
		if !reusable {
			conn.Close()
			if ctxErr := ctx.Err(); ctxErr != nil {
				err = ctxErr
			}
			return
		}
		if hasDeadline {
			conn.SetDeadline(time.Time{})
		}
	}()

	// 发送当前 Bar
	n, err := conn.Write(frame)
	if err != nil {
		return nil, fmt.Errorf("write frame: %w", err)
	}
	if n != len(frame) {
		return nil, fmt.Errorf("write frame: %w", io.ErrShortWrite)
	}

	// 等待 Python Worker 返回确认
	response, err := readFrame(conn)
	if err != nil {
		return nil, err
	}

	var workerResponse WorkerResponse
	if err := json.Unmarshal(response, &workerResponse); err != nil {
		return nil, err
	}

	// 验证 Sequence
	if workerResponse.Seq != msg.Seq {
		return nil, fmt.Errorf(
			"sequence mismatch: expected %d, got %d",
			msg.Seq, workerResponse.Seq,
		)
	}

	switch workerResponse.Condition {
	case "OK":
		// Python 处理完成，没有交易信号
		reusable = true
		return nil, nil

	case "ORDER":
		if len(workerResponse.OrderBatch) == 0 {
			return nil, fmt.Errorf("ORDER response contains no orders")
		}
		reusable = true
		return workerResponse.OrderBatch, nil

	case "ERROR":
		reusable = true
		return nil, fmt.Errorf(
			"worker error at sequence %d",
			workerResponse.Seq,
		)

	default:
		return nil, fmt.Errorf(
			"unknown worker condition: %s",
			workerResponse.Condition,
		)
	}
}

// encodeFrame 加上帧头，并在写之前拒绝超过上限的负载。
func encodeFrame(payload []byte) ([]byte, error) {
	if len(payload) > maxFrameSize {
		return nil, fmt.Errorf("frame payload %d bytes exceeds %d limit", len(payload), maxFrameSize)
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	return frame, nil
}

// readFrame 读取完整一帧;io.ReadFull 处理短读与粘包
// 长度为零或超上限是协议错误。
func readFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("read frame header: %w", err)
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > maxFrameSize {
		return nil, fmt.Errorf("invalid frame length %d (limit %d)", length, maxFrameSize)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("read frame payload: %w", err)
	}
	return payload, nil
}
