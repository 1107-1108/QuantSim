package portfolio

import (
	"fmt"
	"math"
	"time"
)

// EntryType 账本条目类型。
// 账户上发生的每件事都会留下对应类型的条目,账本 = 账户状态的完整审计轨迹:
// 交易明细、胜率/盈亏比、对账,全部从账本聚合而来。
type EntryType string

const (
	// EntryTrade 一笔成交(买或卖)。佣金挂在这个条目上,不单独建条目类型。
	EntryTrade EntryType = "TRADE"

	// EntryRealizedPnL 一个被消耗的 lot 产生的已实现盈亏。
	// 一笔卖出消耗 N 个 lot 就有 N 条;它回答"这笔盈亏来自哪笔买入"(经 LotOpenSeq)。
	EntryRealizedPnL EntryType = "REALIZED_PNL"

	// EntryDividend 现金分红入账。
	EntryDividend EntryType = "DIVIDEND"

	// EntrySplitAdjustment 拆股/反向拆股对持仓的结构调整(不动现金、不产生盈亏)。
	EntrySplitAdjustment EntryType = "SPLIT_ADJUSTMENT"
)

// LedgerEntry 一条账本记录。
// 注意:portfolio 只返回"本次事件新增"的条目,不持有全量账本;
// 全量账本由事件循环累积(回测落 backtest_runs,paper trading 落 paper_fills)。
type LedgerEntry struct {
	// Seq 全局账本序号。portfolio 不管它(返回时恒为 0),
	// 由事件循环在累积条目时统一分配,保证全局有序。
	Seq uint64

	Time   time.Time // Event time.
	Type   EntryType
	Symbol string

	// 以下数值字段按类型取用,不用的恒为 0(对照规格 3.4 的字段使用表):
	Qty         float64
	Price       float64
	Commission  float64
	RealizedPnL float64
	Amount      float64

	// CashAfter 本事件处理完后的账户现金。同一事件产生的所有条目填同一个值,
	// 便于按时间重放账本时逐条对账。
	CashAfter float64

	// FillSeq 来源成交的序号(TRADE / REALIZED_PNL 用),其余为 0。
	FillSeq uint64

	// LotOpenSeq 仅 REALIZED_PNL:被消耗 lot 的 OpenSeq。
	// 用它 join 回创建该 lot 的那条买入 TRADE,就能回答"这笔盈亏来自哪笔买入"。
	LotOpenSeq uint64
}

// VerifyConservation 校验账户的守恒恒等式(账本与状态互相咬合的硬约束):
//
//	现金 + Σ(各标的成本基础)  =  初始现金 + Σ分红 + Σ已实现盈亏
//
// 直觉:流入账户的钱(初始本金、分红、卖出赚的)只能以两种形态存在——
// 要么是现金,要么还压在持仓成本里。恒等式破了,说明记账有 bug。
// 测试(脚本序列 / 随机 property)和调试共用此函数。
//
// 浮点容差:1e-6 × max(1, |initialCash|),资金量级越大容差等比放宽。
func VerifyConservation(initialCash float64, account Account, entries []LedgerEntry) error {
	// 左项:现金 + 全部持仓的成本基础
	lhs := account.cash
	for symbol := range account.positions {
		lhs += account.CostBasis(symbol)
	}

	// 右项的两个账本聚合量
	var dividends, realized float64
	for _, e := range entries {
		switch e.Type {
		case EntryDividend:
			dividends += e.Qty * e.Amount // 持仓量 × 每股分红 = 分红总额
		case EntryRealizedPnL:
			realized += e.RealizedPnL
		}
	}
	rhs := initialCash + dividends + realized

	if diff := math.Abs(lhs - rhs); diff > 1e-6*math.Max(1, math.Abs(initialCash)) {
		return fmt.Errorf("conservation check failed: cash+cost basis %v != initial cash+dividends+realized %v (diff %v)", lhs, rhs, lhs-rhs)
	}
	return nil
}
