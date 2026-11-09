package portfolio

import (
	"fmt"
	"math"

	"FinanceSimulator/datasource"
)

// ApplyCorporateAction 将一项已在除息日生效的公司行为应用到账户。
//
// 调用方负责在策略处理当日 bar 前调用本函数。拆股仅重写 lot 的数量和单位成本，
// 分红直接增加现金；两类事件都遵守 Account 的不可变状态约定。
func ApplyCorporateAction(account Account, action datasource.CorporateAction) (Account, []LedgerEntry, error) {
	if err := validateCorporateAction(action); err != nil {
		return account, nil, err
	}

	held := account.PositionQty(action.Symbol)
	if held == 0 {
		return account, nil, nil
	}

	switch action.Type {
	case datasource.ActionDividend:
		next, entries := applyDividend(account, action, held)
		return next, entries, nil
	case datasource.ActionSplit, datasource.ActionReverseSplit:
		next, entries := applySplit(account, action)
		return next, entries, nil
	default:
		// validateCorporateAction 已覆盖所有类型；保留此分支以保证未来扩展时 fail fast。
		return account, nil, fmt.Errorf("%w: %q", ErrUnknownCorporateAction, action.Type)
	}
}

// validateCorporateAction 拒绝当前账本模型无法准确表达的公司行为输入。
func validateCorporateAction(action datasource.CorporateAction) error {
	switch action.Type {
	case datasource.ActionDividend:
		if math.IsNaN(action.CashPerShare) || math.IsInf(action.CashPerShare, 0) || action.CashPerShare < 0 {
			return fmt.Errorf("%w: invalid dividend amount %v", ErrUnknownCorporateAction, action.CashPerShare)
		}
	case datasource.ActionSplit:
		if math.IsNaN(action.SplitRatio) || math.IsInf(action.SplitRatio, 0) || action.SplitRatio <= 1 {
			return fmt.Errorf("%w: split ratio must be greater than one: %v", ErrUnknownCorporateAction, action.SplitRatio)
		}
	case datasource.ActionReverseSplit:
		if math.IsNaN(action.SplitRatio) || math.IsInf(action.SplitRatio, 0) || action.SplitRatio <= 0 || action.SplitRatio >= 1 {
			return fmt.Errorf("%w: reverse split ratio must be between zero and one: %v", ErrUnknownCorporateAction, action.SplitRatio)
		}
	default:
		return fmt.Errorf("%w: %q", ErrUnknownCorporateAction, action.Type)
	}
	return nil
}

// applyDividend 增加持仓对应的现金分红，并创建一条可供对账的账本条目。
func applyDividend(account Account, action datasource.CorporateAction, held float64) (Account, []LedgerEntry) {
	next := account.clone()
	next.cash += action.CashPerShare * held

	return next, []LedgerEntry{{
		Time:      action.Date,
		Type:      EntryDividend,
		Symbol:    action.Symbol,
		Qty:       held,
		Amount:    action.CashPerShare,
		CashAfter: next.cash,
	}}
}

// applySplit 按公司行为比例调整同一标的的每个 lot，并保持现金和 FIFO 元数据不变。
func applySplit(account Account, action datasource.CorporateAction) (Account, []LedgerEntry) {
	next := account.clone()
	lots := next.positions[action.Symbol]
	for index := range lots {
		lots[index].Qty *= action.SplitRatio
		lots[index].CostPerShare /= action.SplitRatio
	}
	next.positions[action.Symbol] = lots

	return next, []LedgerEntry{{
		Time:      action.Date,
		Type:      EntrySplitAdjustment,
		Symbol:    action.Symbol,
		Qty:       next.PositionQty(action.Symbol),
		Amount:    action.SplitRatio,
		CashAfter: next.cash,
	}}
}
