"""Go ↔ Python worker 的线上契约消息。

线上 JSON 用短键名(seq/o/h/l/c/v),Python 侧对外暴露完整单词属性,
短键名 → 完整名的映射集中在 from_dict,策略代码永远只见完整名。

所有消息都是 frozen dataclass:策略代码拿到的是只读视图。
契约细节见 docs/design/DESIGN-DETAIL.md。
"""

from dataclasses import dataclass
from types import MappingProxyType
from typing import Any, Mapping


@dataclass(frozen=True)
class PositionSnapshot:
    """单个标的的持仓视图:数量 + 均价。"""

    qty: float
    avg_cost: float


@dataclass(frozen=True)
class AccountSnapshot:
    """某一时刻的账户视图,随每根 BarMsg 下发。

    策略的"持仓/现金查询" = 读这份本地缓存,不需要反向 RPC。
    """

    cash: float
    positions: Mapping[str, PositionSnapshot]


@dataclass(frozen=True)
class BarMsg:
    """Go 下发的一根 K 线 + 当时刻账户状态。

    o/h/l/c/v 均为复权后策略口径,复权在推送前由 Go 完成。
    """

    seq: int  # 严格递增序号
    symbol: str
    ts: int  # bar 时间,epoch 毫秒(UTC)
    open: float
    high: float
    low: float
    close: float
    volume: float
    account: AccountSnapshot

    @staticmethod
    def from_dict(data: Mapping[str, Any]) -> "BarMsg":
        """从线上 JSON 字典解析;缺契约键抛 KeyError,在边界处快速失败。"""
        account = data["account"]
        positions = {
            symbol: PositionSnapshot(qty=float(p["qty"]), avg_cost=float(p["avg_cost"]))
            for symbol, p in account["positions"].items()
        }
        return BarMsg(
            seq=int(data["seq"]),
            symbol=str(data["symbol"]),
            ts=int(data["ts"]),
            open=float(data["o"]),
            high=float(data["h"]),
            low=float(data["l"]),
            close=float(data["c"]),
            volume=float(data["v"]),
            account=AccountSnapshot(
                cash=float(account["cash"]),
                positions=MappingProxyType(positions),
            ),
        )
