from typing import Any


class Strategy:
    def on_bar(self, bar: dict[str, Any]) -> list[dict[str, Any]]:
        """根据当前 bar 和账户快照返回订单。

        收盘价是 bar["c"]，现金和持仓在 bar["account"] 里。
        每笔订单包含 symbol、side、type 和 qty，限价单另加 limit_price。
        返回空列表表示这一根 bar 没有订单。
        """
        return []
