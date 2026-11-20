import argparse
import json
import logging
import socket
from typing import Any

from communication import MAX_FRAME_SIZE, connect, read_frame, write_frame
from strategy import Strategy


DEFAULT_SOCKET_PATH = "/tmp/worker.socket"


def _encode_response(
    seq: int, condition: str, orders: list[dict[str, Any]]
) -> bytes:
    response = {"seq": seq, "condition": condition, "orderBatch": orders}
    return json.dumps(response, allow_nan=False, separators=(",", ":")).encode("utf-8")


def run(conn: socket.socket, strategy: Strategy) -> int:
    """逐帧处理行情，处理完当前 bar 后回复 Go。"""
    previous_seq = 0
    while True:
        payload = read_frame(conn)
        if payload is None:
            return 0

        bar = json.loads(payload.decode("utf-8"))
        if not isinstance(bar, dict):
            raise ValueError("bar must be a JSON object")

        seq = bar.get("seq")
        if type(seq) is not int or not 0 < seq < (1 << 64):
            raise ValueError(f"invalid bar sequence: {seq!r}")

        try:
            if seq <= previous_seq:
                raise ValueError(f"bar sequence {seq} must be greater than {previous_seq}")

            orders = strategy.on_bar(bar)
            if not isinstance(orders, list) or any(
                not isinstance(order, dict) for order in orders
            ):
                raise TypeError("on_bar must return a list of order dictionaries")

            condition = "ORDER" if orders else "OK"
            response = _encode_response(seq, condition, orders)
            if len(response) > MAX_FRAME_SIZE:
                raise ValueError(
                    f"response payload {len(response)} bytes exceeds {MAX_FRAME_SIZE} limit"
                )
        except Exception:
            logging.exception("cannot process bar at sequence %d", seq)
            write_frame(conn, _encode_response(seq, "ERROR", []))
            return 1

        write_frame(conn, response)
        previous_seq = seq


def main() -> int:
    parser = argparse.ArgumentParser(description="Run the Python strategy worker.")
    parser.add_argument(
        "--socket",
        default=DEFAULT_SOCKET_PATH,
        help="Go worker socket path (default: %(default)s)",
    )
    args = parser.parse_args()

    try:
        with connect(args.socket) as conn:
            return run(conn, Strategy())
    except (EOFError, OSError, ValueError) as error:
        logging.error("worker: %s", error)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
