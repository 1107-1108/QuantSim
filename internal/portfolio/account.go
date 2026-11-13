package portfolio

import (
	"fmt"
	"math"
	"slices"
	"time"
)

// Lot 表示一次买入后尚未卖出的持仓批次。
//
// 所有字段均导出，确保 paper trading 快照可以经 encoding/json 无损保存和恢复。
// Lot 不保存 symbol：所属标的由 Account.positions 的 map key 表达。
type Lot struct {
	OpenTime     time.Time // Position-open time, used only for display and audit.
	OpenSeq      uint64    // Open-fill sequence; with OpenTime it gives deterministic FIFO order.
	Qty          float64   // Remaining quantity; always positive after a state transition.
	CostPerShare float64   // Per-share cost including buy commission.
}

// Account 是不可变账户状态。外部无法直接访问内部 map 或切片，必须通过
// ApplyFill 和 ApplyCorporateAction 两个状态转换入口创建下一状态。
type Account struct {
	initialCash float64
	cash        float64
	positions   map[string][]Lot
}

// Cash 返回账户当前可用现金。
func (account *Account) Cash() float64 {
	return account.cash
}

// InitialCash 返回账户创建时的初始现金，用于收益基准和守恒校验。
func (account *Account) InitialCash() float64 {
	return account.initialCash
}

// PositionQty 返回一个标的全部未平仓 lot 的数量之和。
func (account *Account) PositionQty(symbol string) float64 {
	var total float64
	for _, lot := range account.positions[symbol] {
		total += lot.Qty
	}
	return total
}

// CostBasis 返回一个标的全部未平仓 lot 的成本基础。
func (account *Account) CostBasis(symbol string) float64 {
	var total float64
	for _, lot := range account.positions[symbol] {
		total += lot.Qty * lot.CostPerShare
	}
	return total
}

// AvgCost 返回展示用的加权平均成本；无持仓时返回零而不是 NaN。
func (account *Account) AvgCost(symbol string) float64 {
	qty := account.PositionQty(symbol)
	if qty == 0 {
		return 0
	}
	return account.CostBasis(symbol) / qty
}

// BuyingPower 返回当前购买力；P0 不支持保证金，因此它等于现金。
func (account *Account) BuyingPower() float64 {
	return account.cash
}

// NewAccount 创建一个没有持仓的账户；初始现金必须是有限且非负的金额。
func NewAccount(initialCash float64) (Account, error) {
	if math.IsNaN(initialCash) || math.IsInf(initialCash, 0) || initialCash < 0 {
		return Account{}, fmt.Errorf("initial cash must be finite and non-negative")
	}
	return Account{
		initialCash: initialCash,
		cash:        initialCash,
		positions:   make(map[string][]Lot),
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
		positions:   make(map[string][]Lot, len(account.positions)),
	}
	for symbol, lots := range account.positions {
		next.positions[symbol] = slices.Clone(lots)
	}
	return next
}
