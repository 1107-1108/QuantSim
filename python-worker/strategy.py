from typing import Any


class Strategy:
    def __init__(self) -> None:
        self.submitted = False

    def on_bar(self, bar: dict[str, Any]) -> list[dict[str, Any]]:
        """首次收到 AAPL 或 MSFT 行情时各买 100 股，之后一直持有。"""
        if self.submitted or bar["symbol"] not in ("AAPL", "MSFT"):
            return []

        self.submitted = True
        return [
            {"symbol": "AAPL", "side": "BUY", "type": "MARKET", "qty": 100},
            {"symbol": "MSFT", "side": "BUY", "type": "MARKET", "qty": 1000},
        ]
