package worker

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

// maxFrameSize 是单帧上限 16 MiB
// 帧格式:4 字节无符号大端长度头 + length 字节 UTF-8 JSON 负载。
const maxFrameSize = 16 << 20

// Session 与每个Worker的通讯
type Session struct {
	socket    net.Listener
	orderList []OrderMsg
}

type WorkerCondition string

const (
	OK           WorkerCondition = "OK"
	NoEnoughData WorkerCondition = "NOENOUGHDATA"
)

type WorkerResponse struct {
	Seq       uint64          `json:"seq"`
	Condition WorkerCondition `json:"condition"`
}

// CreateListener 创建一个Unix Socket
// sockName 文件名
func (s *Session) CreateListener(sockName string) error {
	sPath := fmt.Sprintf("/tmp/%s", sockName)
	_, err := os.Stat(sPath)
	if err == nil {
		log.Fatalln("Socket exists!")
		return err
	}
	socket, err := net.Listen("unix", sPath)
	log.Println("Socket created successfully")
	if err != nil {
		return err
	}
	s.socket = socket
	return nil
}

// OnBar往python推送bar进行运算
func (s *Session) OnBar(msgs []BarMsg) error {
	// 创建 Socket Listener
	if err := s.CreateListener("worker.socket"); err != nil {
		return err
	}
	defer s.socket.Close()

	// 等待唯一的 Python Worker 连接
	conn, err := s.socket.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	// 逐帧发送 Bar
	for _, msg := range msgs {
		// Validity Check
		if msg.Seq == 0 {
			return fmt.Errorf("sequence number cannot be 0")
		}

		// 序列化 BarMsg
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}

		// 发送当前 Bar
		if err := writeFrame(conn, data); err != nil {
			return err
		}

		// 等待 Python Worker 返回确认
		response, err := readFrame(conn)
		if err != nil {
			return err
		}

		var workerResponse struct {
			Condition string `json:"condition"`
			Seq       int    `json:"seq"`
			Error     string `json:"error"`
		}

		if err := json.Unmarshal(response, &workerResponse); err != nil {
			return err
		}

		// 验证 Sequence
		if workerResponse.Seq != int(msg.Seq) {
			return fmt.Errorf("sequence mismatch: expected %d, got %d",
				msg.Seq, workerResponse.Seq)
		}

		switch workerResponse.Condition {
		case "OK":
			// 当前 Bar 处理完成，继续下一帧
			continue

		case "ERROR":
			return fmt.Errorf("worker error: %s", workerResponse.Error)

		default:
			return fmt.Errorf("unknown worker condition: %s",
				workerResponse.Condition)
		}
	}

	return nil
}

// writeFrame 完整写出一帧;长度超上限是协议错误,在写之前拒绝。
func writeFrame(w io.Writer, payload []byte) error {
	if len(payload) > maxFrameSize {
		return fmt.Errorf("frame payload %d bytes exceeds %d limit", len(payload), maxFrameSize)
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	if _, err := w.Write(frame); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	return nil
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
