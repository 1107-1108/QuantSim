package portfolio

import (
	"fmt"
	"math"
	"slices"
	"time"
)

// Lot 批次买入的股票, 也就是一次买入股票的记录
// OriginalQty 和 RemainQty 在拆股时可能冲突
type Lot struct {
	openTime     time.Time
	openSeq      uint64  // 在成交时间相同时，保证 FIFO 顺序确定
	RemainQty    float64 // 经过卖出、拆股等事件调整后的剩余数量
	costPerShare float64 // 整后的单位成本，不一定等于原始买入价
}

type InstrumentID string // 作为账户 map 的 key，而不是每个 Lot 都重复存储 Symbol
// 考虑用Symbol

type Account struct {
	initialCash float64
	cash        float64
	positions   map[InstrumentID][]Lot
}

// 外部无法直接修改Account内部的数据，只能通过其内部Api
// Account不可变，每次买卖股票创建一个新的Account
// Copy-on-write

// Cash 返回账户里的cash
func (account *Account) Cash() float64 {
	return account.cash
}

// PositionQty 对某个股票下所有lot的剩余数量求和
func (account *Account) PositionQty(symbol string) float64 {
	var total float64
	for _, lot := range account.positions[InstrumentID(symbol)] {
		total += lot.RemainQty
	}
	return total
}

// CostBasis 剩余持仓的总成本基础
func (account *Account) CostBasis(symbol string) float64 {
	var total float64
	for _, lot := range account.positions[InstrumentID(symbol)] {
		total += lot.RemainQty * lot.costPerShare
	}
	return total
}

// AvgCost 每股平均成本
func (account *Account) AvgCost(symbol string) float64 {
	res := account.CostBasis(symbol) / account.PositionQty(symbol)
	return res
}

// BuyingPower 你现在还能拿多少钱去买股票
func (account *Account) BuyingPower() float64 {
	return account.cash
}

func NewAccount(initialCash float64) (Account, error) {
	if math.IsNaN(initialCash) || initialCash < 0 {
		err := fmt.Errorf("initialCash can't be negative or NaN")
		return Account{}, err
	}
	return Account{
		initialCash: initialCash,
		cash:        initialCash,
		positions:   make(map[InstrumentID][]Lot),
	}, nil
}

// clone 深拷贝账户:不可变性的唯一兜底。
// Account 的每次状态变更都"先 clone,再在副本上改",原账户保持不动。
// 注意 map 和 slice 都是引用类型,只做结构体值拷贝的话,新旧账户会共享底层数据,
// 改新账户就会污染旧账户——所以必须新建 map、逐标的拷贝 lot 切片。
// Lot 是纯值类型(不含指针/切片/map),切片拷贝即为深拷贝。
func (account *Account) clone() Account {
	next := Account{
		initialCash: account.initialCash,
		cash:        account.cash,
		positions:   make(map[InstrumentID][]Lot, len(account.positions)),
	}
	for symbol, lots := range account.positions {
		next.positions[symbol] = slices.Clone(lots)
	}
	return next
}
