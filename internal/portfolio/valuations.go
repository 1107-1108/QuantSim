package portfolio

import (
	"fmt"
	"slices"
)

// Equity 账户净值 = 现金 + Σ(持仓数量 × 市价)
// 持仓标的缺 mark → ErrMissingMark;数量为 0 的持仓跳过,不要求 mark
func (account *Account) Equity(marks map[InstrumentID]float64) (float64, error) {
	total := account.cash
	for symbol := range account.positions {
		qty := account.PositionQty(string(symbol))
		if qty == 0 {
			continue
		}
		mark, ok := marks[symbol]
		if !ok {
			return 0.0, fmt.Errorf("%w: %s", ErrMissingMark, symbol)
		}
		total += qty * mark
	}
	return total, nil
}

// MarketValue 市值
func (account *Account) MarketValue(symbol string, mark float64) float64 {
	return account.PositionQty(symbol) * mark
}

// UnrealizedPnL 未实现盈亏/浮动盈亏:Σ(市值 − 成本基础)
// 持仓标的缺 mark → ErrMissingMark;数量为 0 的持仓跳过,不要求 mark
func (account *Account) UnrealizedPnL(marks map[InstrumentID]float64) (float64, error) {
	var total float64
	for symbol := range account.positions {
		qty := account.PositionQty(string(symbol))
		if qty == 0 {
			continue
		}
		mark, ok := marks[symbol]
		if !ok {
			return 0.0, fmt.Errorf("%w: %s", ErrMissingMark, symbol)
		}
		total += qty*mark - account.CostBasis(string(symbol))
	}
	return total, nil
}

// PositionView 单个标的的持仓投影
type PositionView struct {
	Symbol        InstrumentID
	Qty           float64
	AvgCost       float64
	Mark          float64
	MarketValue   float64
	UnrealizedPnL float64
}

// AccountView 账户视图:给前端的一页快照;持仓按 symbol 排序,输出确定
type AccountView struct {
	Cash          float64
	Equity        float64
	CostBasis     float64
	UnrealizedPnL float64
	Positions     []PositionView
}

// AccountView 汇总账户级与各持仓的展示数据
// 持仓标的缺 mark → ErrMissingMark;数量为 0 的持仓跳过
func (account *Account) AccountView(marks map[InstrumentID]float64) (AccountView, error) {
	view := AccountView{Cash: account.cash, Equity: account.cash}

	symbols := make([]InstrumentID, 0, len(account.positions))
	for symbol := range account.positions {
		symbols = append(symbols, symbol)
	}
	slices.Sort(symbols)

	for _, symbol := range symbols {
		qty := account.PositionQty(string(symbol))
		if qty == 0 {
			continue
		}
		mark, ok := marks[symbol]
		if !ok {
			return AccountView{}, fmt.Errorf("%w: %s", ErrMissingMark, symbol)
		}
		costBasis := account.CostBasis(string(symbol))
		marketValue := qty * mark
		unrealizedPnL := marketValue - costBasis

		view.Positions = append(view.Positions, PositionView{
			Symbol:        symbol,
			Qty:           qty,
			AvgCost:       account.AvgCost(string(symbol)),
			Mark:          mark,
			MarketValue:   marketValue,
			UnrealizedPnL: unrealizedPnL,
		})
		view.CostBasis += costBasis
		view.UnrealizedPnL += unrealizedPnL
		view.Equity += marketValue
	}
	return view, nil
}
