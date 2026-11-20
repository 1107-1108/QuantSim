package worker

// Go ↔ Python worker 的线上契约消息。
//
// 契约要点(详见 docs/design/DESIGN-DETAIL.md):
//   - o/h/l/c/v 均为策略口径(复权后),复权在推送前由 Go 完成,消息不含 AdjClose
//   - 账户快照随每根 BarMsg 下发,worker 查持仓 = 读本地缓存,无反向 RPC
//   - 字段短键名(seq/o/h/l/c/v)是契约的一部分,Python 端按这些键解析,不得改名
//
// BarMsg 与内部 datasource.Bar 刻意分离:内部类型的演化不应破坏线上契约。

// BarMsg 是下发给 Python worker 的一根 K 线 + 当时刻账户状态。
type BarMsg struct {
	Seq     uint64          `json:"seq"`    // 严格递增序号;worker 按序分发,paper 会话断点续传的锚点, 非0
	Symbol  string          `json:"symbol"` // 内部 Bar 不带 symbol(存储按 symbol 分区),线上多标的混流必须带
	Ts      int64           `json:"ts"`     // bar 时间,epoch 毫秒(UTC)
	Open    float64         `json:"o"`      // 以下五个字段均为复权后策略口径
	High    float64         `json:"h"`
	Low     float64         `json:"l"`
	Close   float64         `json:"c"`
	Volume  float64         `json:"v"`       // 拆股后数量可能是非整数,不用 int64
	Account AccountSnapshot `json:"account"` // 当时刻账户快照
}

// AccountSnapshot 是某一时刻的账户视图:现金 + 各标的数量/均价。
// 数据来自 portfolio.Account 的 Cash()/PositionQty()/AvgCost()。
type AccountSnapshot struct {
	Cash      float64                     `json:"cash"`
	Positions map[string]PositionSnapshot `json:"positions"`
}

// PositionSnapshot 是单个标的的持仓视图。
type PositionSnapshot struct {
	Qty     float64 `json:"qty"`
	AvgCost float64 `json:"avg_cost"`
}

// NewAccountSnapshot 创建无持仓的账户快照。
// Positions 必须初始化为空 map:Python 端按 dict 解析,nil map 会序列化成 null。
func NewAccountSnapshot(cash float64) AccountSnapshot {
	return AccountSnapshot{
		Cash:      cash,
		Positions: make(map[string]PositionSnapshot),
	}
}

// OrderMsg 是策略经 on_bar 响应返回的订单。
// side/type 用裸字符串保持契约简单;合法性校验是事件循环的职责,不在传输层做。
type OrderMsg struct {
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"` // BUY / SELL
	Type       string  `json:"type"` // MARKET / LIMIT
	Qty        float64 `json:"qty"`
	LimitPrice float64 `json:"limit_price,omitempty"` // 仅 LIMIT 单有意义
}

// OnBarResult 是 on_bar 响应的 result 负载:本次钩子返回的订单列表。
// 订单归属绑定到"返回它的那次 on_bar 调用"(§7.4),故无需额外的 uptoSeq 字段。
type OnBarResult struct {
	Orders []OrderMsg `json:"orders"`
}
