package backtest

import (
	"context"

	"FinanceSimulator/datasource"
	"FinanceSimulator/matcher"
	"FinanceSimulator/portfolio"
	"FinanceSimulator/worker"
)

// RunPython 使用一个 Python worker 执行整个回测的策略回调。session 为 nil 时使用默认配置。
// 当前将 Run 接收的成交时原价行情原样发给 Python。策略订单仍由 Run 校验、排队和撮合。
func RunPython(ctx context.Context, account portfolio.Account, bars []SymbolBar, session *worker.Session, splits ...datasource.CorporateAction) (Result, error) {
	if session == nil {
		session = &worker.Session{}
	}

	var result Result
	err := session.WithWorker(ctx, func(sendBar worker.SendBar) error {
		var seq uint64
		onBar := func(_ context.Context, item SymbolBar, account portfolio.Account) ([]matcher.Order, error) {
			seq++
			// Python 收到当前标的的行情，以及同一时间点全部成交后的组合持仓。
			snapshot := worker.NewAccountSnapshot(account.Cash())
			for _, symbol := range account.PositionSymbols() {
				snapshot.Positions[symbol] = worker.PositionSnapshot{
					Qty:     account.PositionQty(symbol),
					AvgCost: account.AvgCost(symbol),
				}
			}
			bar := item.Bar
			orders, err := sendBar(worker.BarMsg{
				Seq:     seq,
				Symbol:  item.Symbol,
				Ts:      bar.Time.UnixMilli(),
				Open:    bar.Open,
				High:    bar.High,
				Low:     bar.Low,
				Close:   bar.Close,
				Volume:  float64(bar.Volume),
				Account: snapshot,
			})
			if err != nil || len(orders) == 0 {
				return nil, err
			}
			// 转回 Go 订单结构，由回测循环给它们分配 ID，并在下一根处理。
			intents := make([]matcher.Order, 0, len(orders))
			for _, order := range orders {
				intents = append(intents, matcher.Order{
					Symbol:     order.Symbol,
					Type:       matcher.OrderType(order.Type),
					Side:       portfolio.Side(order.Side),
					Qty:        order.Qty,
					LimitPrice: order.LimitPrice,
				})
			}
			return intents, nil
		}
		var runErr error
		result, runErr = Run(ctx, account, bars, onBar, splits...)
		return runErr
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}
