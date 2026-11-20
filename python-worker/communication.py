import socket
import struct
from collections.abc import Iterator
from contextlib import contextmanager
from typing import Optional


MAX_FRAME_SIZE = 16 << 20
FRAME_HEADER = struct.Struct("!I")


@contextmanager
def connect(socket_path: str) -> Iterator[socket.socket]:
    """连接 Go 监听的 Unix socket。"""
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as conn:
        conn.connect(socket_path)
        yield conn


def _read_bytes(conn: socket.socket, size: int) -> bytes:
    """读满指定长度，遇到 EOF 时返回已读的字节。"""
    data = bytearray()
    while len(data) < size:
        chunk = conn.recv(size - len(data))
        if not chunk:
            break
        data.extend(chunk)
    return bytes(data)


def read_frame(conn: socket.socket) -> Optional[bytes]:
    """读取一帧。Go 在两帧之间关闭连接时返回 None。"""
    header = _read_bytes(conn, FRAME_HEADER.size)
    if not header:
        return None
    if len(header) != FRAME_HEADER.size:
        raise EOFError("incomplete frame header")

    (length,) = FRAME_HEADER.unpack(header)
    if length == 0 or length > MAX_FRAME_SIZE:
        raise ValueError(f"invalid frame length {length} (limit {MAX_FRAME_SIZE})")

    payload = _read_bytes(conn, length)
    if len(payload) != length:
        raise EOFError("incomplete frame payload")
    return payload


def write_frame(conn: socket.socket, payload: bytes) -> None:
    """写入 4 字节大端长度头和完整负载。"""
    length = len(payload)
    if length == 0 or length > MAX_FRAME_SIZE:
        raise ValueError(f"invalid frame length {length} (limit {MAX_FRAME_SIZE})")
    conn.sendall(FRAME_HEADER.pack(length) + payload)
