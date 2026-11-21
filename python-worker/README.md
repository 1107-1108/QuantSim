# Python 策略编程指南

在 [`strategy.py`](strategy.py) 里定义 `Strategy` 类和 `on_bar(self, bar)`。每次回测通过 `Strategy()` 创建一个实例，多个标的共用这个实例和同一个组合账户。用 `self` 保存指标、历史行情和策略自己的状态。

从仓库根目录运行回测。标的、日期和初始现金配置在 [`internal/main.go`](../internal/main.go)，Go 会自动用 `python3` 启动 worker。

```bash
go -C internal run .
```

## 输入 `bar`

每次调用收到一个字典，其中只有当前标的的一根已收盘 K 线。账户快照包含组合的全部持仓。

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `seq` | `int` | 本次 worker 会话的消息序号，从 1 开始递增。每条消息都占一个序号。 |
| `symbol` | `str` | 当前 K 线的标的，例如 `AAPL`。 |
| `ts` | `int` | K 线时间，Unix 时间戳，单位为毫秒。同一时间点的多个标的可以有相同的 `ts`。 |
| `o`、`h`、`l`、`c` | 数值 | 开盘、最高、最低、收盘价。当前回测传入成交时原价。 |
| `v` | 数值 | 成交量，沿用数据源口径。 |
| `account.cash` | 数值 | 当前账户可用现金。 |
| `account.positions` | `dict` | 按标的索引的持仓。每项包含 `qty`（股数）和 `avg_cost`（每股平均成本）。空仓时为 `{}`。 |

查询某个标的的持仓时用 `.get()`，因为空仓标的会从 `positions` 中省略。将账户快照当作只读数据，交易请求通过返回订单提交。

```python
account = bar["account"]
cash = account["cash"]
position = account["positions"].get("AAPL", {})
qty_held = position.get("qty", 0)
```

## 输出订单

`on_bar` 必须返回 `list[dict]`。本根无需下单时返回 `[]`。要同时交易多个标的，就返回多个订单字典。

| 字段 | 当前回测要求 |
| --- | --- |
| `symbol` | 要交易的标的，必须在本次回测的行情中。可以交易不同于 `bar["symbol"]` 的标的。 |
| `side` | `"BUY"` 或 `"SELL"`，区分大小写。 |
| `type` | `"MARKET"`。当前回测支持市价单。 |
| `qty` | 股数，使用有限且严格大于零的数值。卖单的数量也填正数。 |

跳过某笔订单时，直接把它从列表中省略。`qty: 0` 会触发 Go 的订单校验并中止回测。订单 ID 由 Go 按提交顺序分配。

## 成交和账户时序

1. 每个时间点，Go 先应用已生效的拆股，再用各标的开盘价撮合旧订单。
2. 同一时间点的旧订单处理完后，Go 才逐条调用策略。这个时间点的各次调用看到同一份更新后的组合账户。
3. 本次返回的新订单进入队列，等**订单所属标的的下一根 K 线**开盘才可能成交。回测结束时，仍在等待行情的订单计为未成交。

多个订单在同一时间点可成交时，Go 按提交顺序处理。资金不足或卖出数量超过持仓时，Go 会记录拒单并继续回测。用当前收盘价估算仓位时，要留意下一根开盘价可能变化。`on_bar` 发出的订单只是意图，可以在后续账户快照里确认持仓变化。

当前 MVP 按零费用、零滑点撮合。拆股生效时 Go 会换算持仓和跨拆股的旧市价挂单。跨拆股计算均线或收益率时，策略需要统一历史价格口径。当前净值口径尚未计入现金分红。

## 示例一 只买一次 AAPL

把以下代码放进 `strategy.py`。第一次收到 AAPL 行情时提交一笔市价单，后续行情返回 `[]`。

```python
class Strategy:
    def __init__(self):
        self.submitted = False

    def on_bar(self, bar):
        if self.submitted or bar["symbol"] != "AAPL":
            return []

        self.submitted = True
        return [{"symbol": "AAPL", "side": "BUY", "type": "MARKET", "qty": 1}]
```

## 示例二 等两只股票的同刻行情

如果策略的决策依赖 AAPL 和 MSFT，就先在 `self.latest` 里保存两只股票最后收到的 K 线。只在两根 K 线的 `ts` 相等时做决定。下例在首次对齐后各提交一笔买单。

```python
class Strategy:
    def __init__(self):
        self.symbols = ("AAPL", "MSFT")
        self.latest = {}
        self.submitted = False

    def on_bar(self, bar):
        symbol = bar["symbol"]
        if self.submitted or symbol not in self.symbols:
            return []

        self.latest[symbol] = bar
        if any(self.latest.get(ticker, {}).get("ts") != bar["ts"] for ticker in self.symbols):
            return []

        self.submitted = True
        return [
            {"symbol": ticker, "side": "BUY", "type": "MARKET", "qty": 1}
            for ticker in self.symbols
        ]
```

即使两个标的的信号在同一时间点生成，它们也按各自的下一根 K 线成交。行情有缺口时，两笔订单可能在不同时间成交。

## 排查常见问题

- **回测完成但成交为零**。先检查策略是否返回了订单。两条连续的「当前标的不是 AAPL 就返回」和「当前标的不是 MSFT 就返回」会拦住所有行情。允许两者通过时，用 `symbol in ("AAPL", "MSFT")` 判断。
- **报 `qty must be positive`**。检查是否返回了 `qty: 0`、负数或非法数值。用 `[]` 表示本根无单。
- **报 `worker error at sequence N`**。查看终端中 Python 的 traceback，对照 `seq` 找到出错的那次调用。单根行情默认有 5 秒的通信超时，避免在 `on_bar` 里阻塞或反复读取整段历史。
- **现金减少但净值上涨**。买入会扣现金。期末净值包含现金和持仓市值。

接口实现见 [Go 消息定义](../internal/worker/messages.go)、[回测引擎](../internal/backtest/engine.go) 和 [Python worker](main.py)。
