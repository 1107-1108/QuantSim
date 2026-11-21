package main

import (
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"FinanceSimulator/backtest"
	"FinanceSimulator/datasource"
	"FinanceSimulator/portfolio"
)

func main() {
	initialCash := 1000000.0
	account, err := portfolio.NewAccount(initialCash)
	if err != nil {
		log.Fatalln(err)
	}
	symbols := []string{"AAPL", "MSFT"}
	start_date := "2006-01-02"
	end_date := "2020-12-11"
	start, err := time.Parse(time.DateOnly, start_date)
	if err != nil {
		log.Fatal(err)
	}
	end, err := time.Parse(time.DateOnly, end_date)
	if err != nil {
		log.Fatal(err)
	}
	if !start.Before(end) {
		log.Fatal("start_date 必须早于 end_date")
	}

	// Yahoo 会用较新的拆股调整旧价格，所以取数范围要覆盖回测结束后的拆股。
	fetchEnd := time.Now().UTC().AddDate(0, 0, 1)
	if end.After(fetchEnd) {
		fetchEnd = end
	}
	yahooRawData, err := datasource.YahooFetch(symbols, start_date, fetchEnd.Format(time.DateOnly), "1d")
	if err != nil {
		log.Fatal(err)
	}
	barList, splits, err := collectBacktestData(yahooRawData, start, end)
	if err != nil {
		log.Fatal(err)
	}

	result, err := backtest.RunPython(context.Background(), account, barList, nil, splits...)
	if err != nil {
		log.Fatal(err)
	}
	finalEquity := result.Equity[len(result.Equity)-1].Value
	log.Printf("回测完成。标的 %v，行情 %d 根，净值点 %d 个。", symbols, len(barList), len(result.Equity))
	log.Printf("初始现金 %.2f，期末现金 %.2f，期末净值 %.2f，成交 %d 笔，拒单 %d 笔，未成交 %d 笔。",
		initialCash, result.Account.Cash(), finalEquity, len(result.Fills), len(result.RejectedOrders), len(result.UnfilledOrders))
}

func collectBacktestData(responses []datasource.YahooResponse, start, end time.Time) ([]backtest.SymbolBar, []datasource.CorporateAction, error) {
	var barList []backtest.SymbolBar
	var splitEvents []datasource.CorporateAction
	for _, response := range responses {
		info, err := response.Symbols()
		if err != nil {
			return nil, nil, err
		}
		if info.Symbol == "" {
			return nil, nil, fmt.Errorf("Yahoo 响应缺少标的代码")
		}
		actions, err := response.CorporateActions()
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", info.Symbol, err)
		}
		var splits []datasource.CorporateAction
		for _, action := range actions {
			if action.Type == datasource.ActionSplit || action.Type == datasource.ActionReverseSplit {
				// 日线按交易所当地拆股日生效，拆股当天的 K 线已经采用拆股后的股数口径。
				date := action.Date
				action.Date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
				splits = append(splits, action)
				// 日期参数表示交易日。只有区间内生效的拆股会进入账户事件循环。
				day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
				if !day.Before(start) && day.Before(end) {
					splitEvents = append(splitEvents, action)
				}
			}
		}

		before := len(barList)
		for bar, err := range response.Bars() {
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", info.Symbol, err)
			}
			// 只把用户指定区间内的行情发给策略，end_date 不包含在内。
			if bar.Time.Before(start) || !bar.Time.Before(end) {
				continue
			}
			factor := 1.0
			for _, split := range splits {
				if bar.Time.Before(split.Date) {
					factor *= split.SplitRatio
				}
			}
			asTraded, err := bar.AsTraded(factor)
			if err != nil {
				return nil, nil, fmt.Errorf("%s %s: %w", info.Symbol, bar.Time.Format(time.DateOnly), err)
			}
			barList = append(barList, backtest.SymbolBar{Symbol: info.Symbol, Bar: asTraded})
		}
		if len(barList) == before {
			return nil, nil, fmt.Errorf("%s 在指定回测区间没有行情", info.Symbol)
		}
	}
	if len(barList) == 0 {
		return nil, nil, fmt.Errorf("没有可用于回测的行情")
	}
	// 合并多标行情，同一时刻保留请求标的的先后顺序。
	slices.SortStableFunc(barList, func(a, b backtest.SymbolBar) int {
		return a.Bar.Time.Compare(b.Bar.Time)
	})
	return barList, splitEvents, nil
}
