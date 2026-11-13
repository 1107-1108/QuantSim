package storage

import (
	"FinanceSimulator/datasource"
	"fmt"
	"math"
	"sort"
	"time"
)

const (
	missingTradingDayThreshold = 5
	splitDeviationThreshold    = 0.15
	priceMoveThreshold         = 0.50
)

// QualitySeverity 表示数据质量问题的严重级别。
type QualitySeverity string

const (
	// QualitySeverityError 表示会影响数据正确性的错误。
	QualitySeverityError QualitySeverity = "error"
	// QualitySeverityWarning 表示需要关注但不会阻断导入的警告。
	QualitySeverityWarning QualitySeverity = "warning"
)

// QualityIssueCode 表示可供程序稳定识别的数据质量问题类型。
type QualityIssueCode string

const (
	// QualityIssueInvalidOHLC 表示开高低收价格关系不合法或包含非有限值。
	QualityIssueInvalidOHLC QualityIssueCode = "invalid_ohlc"
	// QualityIssueNegativeVolume 表示成交量为负数。
	QualityIssueNegativeVolume QualityIssueCode = "negative_volume"
	// QualityIssueTimestampOrder 表示时间戳重复或未严格递增。
	QualityIssueTimestampOrder QualityIssueCode = "timestamp_order"
	// QualityIssueMissingTradingDays 表示连续缺失超过阈值的开市日。
	QualityIssueMissingTradingDays QualityIssueCode = "missing_trading_days"
	// QualityIssueSplitPriceMismatch 表示拆股前后的价格比例不合理。
	QualityIssueSplitPriceMismatch QualityIssueCode = "split_price_mismatch"
	// QualityIssueAbnormalPriceMove 表示异常的日收盘价波动。
	QualityIssueAbnormalPriceMove QualityIssueCode = "abnormal_price_move"
)

// DataQualityIssue 描述一条数据质量校验结果。
type DataQualityIssue struct {
	Severity QualitySeverity  `json:"severity"`
	Code     QualityIssueCode `json:"code"`
	Time     time.Time        `json:"time"`
	Message  string           `json:"message"`
}

// ValidateMarketData 校验行情数据并收集所有问题，不会修改输入切片。
func ValidateMarketData(
	bars []datasource.Bar,
	actions []datasource.CorporateAction,
	calendar []datasource.TradingDay,
) []DataQualityIssue {
	issues := make([]DataQualityIssue, 0)
	for index, bar := range bars {
		if !validOHLC(bar) {
			issues = append(issues, DataQualityIssue{
				Severity: QualitySeverityError,
				Code:     QualityIssueInvalidOHLC,
				Time:     bar.Time,
				Message:  fmt.Sprintf("bar %d has invalid OHLC values", index),
			})
		}
		if bar.Volume < 0 {
			issues = append(issues, DataQualityIssue{
				Severity: QualitySeverityError,
				Code:     QualityIssueNegativeVolume,
				Time:     bar.Time,
				Message:  fmt.Sprintf("bar %d has negative volume", index),
			})
		}
		if index > 0 && !bar.Time.After(bars[index-1].Time) {
			issues = append(issues, DataQualityIssue{
				Severity: QualitySeverityError,
				Code:     QualityIssueTimestampOrder,
				Time:     bar.Time,
				Message:  fmt.Sprintf("bar %d timestamp is not strictly increasing", index),
			})
		}
	}
	issues = append(issues, findMissingTradingDays(bars, calendar)...)
	issues = append(issues, findSplitPriceMismatches(bars, actions)...)
	issues = append(issues, findAbnormalPriceMoves(bars, actions)...)

	return issues
}

// validOHLC 判断一根 K 线的开高低收价格是否有限且关系正确。
func validOHLC(bar datasource.Bar) bool {
	if !finitePrice(bar.Open) || !finitePrice(bar.High) || !finitePrice(bar.Low) || !finitePrice(bar.Close) {
		return false
	}
	return bar.High >= math.Max(bar.Open, bar.Close) &&
		bar.Low <= math.Min(bar.Open, bar.Close) &&
		bar.High >= bar.Low
}

// finitePrice 判断价格是否既不是 NaN 也不是无穷大。
func finitePrice(price float64) bool {
	return !math.IsNaN(price) && !math.IsInf(price, 0)
}

// findMissingTradingDays 找出超过配置阈值的连续缺失开市日。
func findMissingTradingDays(bars []datasource.Bar, calendar []datasource.TradingDay) []DataQualityIssue {
	if len(bars) == 0 || len(calendar) == 0 {
		return nil
	}

	sortedBars := sortedBars(bars)
	firstDate := calendarDate(sortedBars[0].Time)
	lastDate := calendarDate(sortedBars[len(sortedBars)-1].Time)
	barDates := make(map[string]struct{}, len(bars))
	for _, bar := range bars {
		barDates[calendarDateKey(bar.Time)] = struct{}{}
	}

	sortedCalendar := append([]datasource.TradingDay(nil), calendar...)
	sort.Slice(sortedCalendar, func(left, right int) bool {
		return sortedCalendar[left].Date.Before(sortedCalendar[right].Date)
	})
	issues := make([]DataQualityIssue, 0)
	missingCount := 0
	var missingStart time.Time
	previousDateKey := ""
	for _, day := range sortedCalendar {
		dayDate := calendarDate(day.Date)
		dayKey := calendarDateKey(dayDate)
		if dayKey == previousDateKey {
			continue
		}
		previousDateKey = dayKey
		if !day.IsTradingDay {
			continue
		}
		if dayDate.Before(firstDate) || dayDate.After(lastDate) {
			issues = appendMissingTradingDaysIssue(issues, missingStart, missingCount)
			missingCount = 0
			continue
		}
		if _, found := barDates[dayKey]; found {
			issues = appendMissingTradingDaysIssue(issues, missingStart, missingCount)
			missingCount = 0
			continue
		}
		if missingCount == 0 {
			missingStart = dayDate
		}
		missingCount++
	}
	return appendMissingTradingDaysIssue(issues, missingStart, missingCount)
}

// appendMissingTradingDaysIssue 在缺失连续日超过阈值时追加一条警告。
func appendMissingTradingDaysIssue(issues []DataQualityIssue, start time.Time, count int) []DataQualityIssue {
	if count <= missingTradingDayThreshold {
		return issues
	}
	return append(issues, DataQualityIssue{
		Severity: QualitySeverityWarning,
		Code:     QualityIssueMissingTradingDays,
		Time:     start,
		Message:  fmt.Sprintf("%d consecutive trading days are missing", count),
	})
}

// findSplitPriceMismatches 比较拆股日前后的收盘价。
func findSplitPriceMismatches(bars []datasource.Bar, actions []datasource.CorporateAction) []DataQualityIssue {
	if len(bars) == 0 || len(actions) == 0 {
		return nil
	}

	sortedBars := sortedBars(bars)
	sortedActions := append([]datasource.CorporateAction(nil), actions...)
	sort.Slice(sortedActions, func(left, right int) bool {
		return sortedActions[left].Date.Before(sortedActions[right].Date)
	})
	issues := make([]DataQualityIssue, 0)
	for _, action := range sortedActions {
		if !isSplitAction(action) || !finitePrice(action.SplitRatio) || action.SplitRatio <= 0 {
			continue
		}
		preSplit, postSplit, found := adjacentSplitBars(sortedBars, action.Date)
		if !found || !finitePrice(preSplit.Close) || !finitePrice(postSplit.Close) || preSplit.Close == 0 {
			continue
		}
		expectedRatio := 1 / action.SplitRatio
		actualRatio := postSplit.Close / preSplit.Close
		if math.Abs(actualRatio/expectedRatio-1) <= splitDeviationThreshold {
			continue
		}
		issues = append(issues, DataQualityIssue{
			Severity: QualitySeverityWarning,
			Code:     QualityIssueSplitPriceMismatch,
			Time:     action.Date,
			Message:  fmt.Sprintf("split price ratio %.4f differs from expected %.4f", actualRatio, expectedRatio),
		})
	}
	return issues
}

// adjacentSplitBars 返回拆股日前及当日或之后最近的 K 线。
func adjacentSplitBars(bars []datasource.Bar, splitDate time.Time) (datasource.Bar, datasource.Bar, bool) {
	var preSplit datasource.Bar
	foundPreSplit := false
	for _, bar := range bars {
		if bar.Time.Before(splitDate) {
			preSplit = bar
			foundPreSplit = true
			continue
		}
		if foundPreSplit {
			return preSplit, bar, true
		}
	}
	return datasource.Bar{}, datasource.Bar{}, false
}

// findAbnormalPriceMoves 找出没有对应拆股的异常日间波动。
func findAbnormalPriceMoves(bars []datasource.Bar, actions []datasource.CorporateAction) []DataQualityIssue {
	dailyBars := dailyClosingBars(sortedBars(bars))
	if len(dailyBars) < 2 {
		return nil
	}
	splitDates := make(map[string]struct{})
	for _, action := range actions {
		if isSplitAction(action) {
			splitDates[calendarDateKey(action.Date)] = struct{}{}
		}
	}

	issues := make([]DataQualityIssue, 0)
	for index := 1; index < len(dailyBars); index++ {
		previous := dailyBars[index-1]
		current := dailyBars[index]
		if !finitePrice(previous.Close) || !finitePrice(current.Close) || previous.Close == 0 {
			continue
		}
		priceMove := math.Abs(current.Close/previous.Close - 1)
		if priceMove <= priceMoveThreshold {
			continue
		}
		if _, isSplitDate := splitDates[calendarDateKey(current.Time)]; isSplitDate {
			continue
		}
		issues = append(issues, DataQualityIssue{
			Severity: QualitySeverityWarning,
			Code:     QualityIssueAbnormalPriceMove,
			Time:     current.Time,
			Message:  fmt.Sprintf("daily close moved %.2f%%", priceMove*100),
		})
	}
	return issues
}

// sortedBars 返回按时间排序的 bars 副本。
func sortedBars(bars []datasource.Bar) []datasource.Bar {
	sorted := append([]datasource.Bar(nil), bars...)
	sort.SliceStable(sorted, func(left, right int) bool {
		return sorted[left].Time.Before(sorted[right].Time)
	})
	return sorted
}

// dailyClosingBars 返回每个自然日最后一根按时间排列的 K 线。
func dailyClosingBars(bars []datasource.Bar) []datasource.Bar {
	dailyBars := make([]datasource.Bar, 0, len(bars))
	for _, bar := range bars {
		if len(dailyBars) == 0 || calendarDateKey(dailyBars[len(dailyBars)-1].Time) != calendarDateKey(bar.Time) {
			dailyBars = append(dailyBars, bar)
			continue
		}
		dailyBars[len(dailyBars)-1] = bar
	}
	return dailyBars
}

// calendarDate 保留日期组成部分并返回 UTC 零点时间。
func calendarDate(timestamp time.Time) time.Time {
	return time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), 0, 0, 0, 0, time.UTC)
}

// calendarDateKey 返回用于多类输入日期匹配的稳定键。
func calendarDateKey(timestamp time.Time) string {
	return calendarDate(timestamp).Format(time.DateOnly)
}

// isSplitAction 判断公司行为是否为正向或反向拆股。
func isSplitAction(action datasource.CorporateAction) bool {
	return action.Type == datasource.ActionSplit || action.Type == datasource.ActionReverseSplit
}
