"""BarMsg 契约消息的 Python 侧解析测试。

WIRE 字典与 Go 侧 messages_test.go 的 sampleBarMsg 严格同构,
保证两端对同一份线上消息的理解一致。
"""

import dataclasses
import json

import pytest

from protocol import BarMsg

WIRE = {
    "seq": 42,
    "symbol": "AAPL",
    "ts": 1737000000000,
    "o": 100.5,
    "h": 101.0,
    "l": 99.5,
    "c": 100.8,
    "v": 12345.5,
    "account": {
        "cash": 50000.25,
        "positions": {
            "AAPL": {"qty": 100, "avg_cost": 99.5},
            "MSFT": {"qty": 50, "avg_cost": 300.1},
        },
    },
}


def test_from_dict_parses_wire_message():
    # Arrange — 模拟从 JSON-RPC 通道收到的字节流
    raw = json.loads(json.dumps(WIRE))

    # Act
    msg = BarMsg.from_dict(raw)

    # Assert
    assert msg.seq == 42
    assert msg.symbol == "AAPL"
    assert msg.ts == 1737000000000
    assert msg.open == 100.5
    assert msg.high == 101.0
    assert msg.low == 99.5
    assert msg.close == 100.8
    assert msg.volume == 12345.5
    assert msg.account.cash == 50000.25
    assert msg.account.positions["AAPL"].qty == 100
    assert msg.account.positions["AAPL"].avg_cost == 99.5
    assert msg.account.positions["MSFT"].qty == 50


def test_bar_msg_is_immutable():
    # Arrange
    msg = BarMsg.from_dict(WIRE)

    # Act & Assert — 字段不可重新赋值
    with pytest.raises(dataclasses.FrozenInstanceError):
        msg.seq = 1

    # Act & Assert — positions 字典本身也不可变
    with pytest.raises(TypeError):
        msg.account.positions["MSFT"] = msg.account.positions["AAPL"]


def test_from_dict_missing_contract_key_raises():
    # Arrange — 缺 c 键,边界处必须立刻失败,不许静默吞错
    broken = {key: value for key, value in WIRE.items() if key != "c"}

    # Act & Assert
    with pytest.raises(KeyError):
        BarMsg.from_dict(broken)


def test_from_dict_empty_positions():
    # Arrange — Go 侧保证空持仓序列化为 {} 而不是 null
    wire = dict(WIRE)
    wire["account"] = {"cash": 10000, "positions": {}}

    # Act
    msg = BarMsg.from_dict(wire)

    # Assert
    assert msg.account.cash == 10000
    assert len(msg.account.positions) == 0
