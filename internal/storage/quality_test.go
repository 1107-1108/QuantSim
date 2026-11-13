package storage

import (
	"FinanceSimulator/datasource"
	"testing"
	"time"
)

// TestQualityOHLC 验证非法价格和负成交量会生成错误问题。
func TestQualityOHLC(t *testing.T) {
	baseTime := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	bars := []datasource.Bar{
		{Time: baseTime, Open: 10, High: 9, Low: 8, Close: 10, Volume: 100},
		{Time: baseTime.AddDate(0, 0, 1), Open: 10, High: 11, Low: 9, Close: 10, Volume: -1},
	}

	issues := ValidateMarketData(bars, nil, nil)
	if !containsQualityIssue(issues, QualityIssueInvalidOHLC, QualitySeverityError) {
		t.Errorf("issues = %#v, want invalid OHLC error", issues)
	}
	if !containsQualityIssue(issues, QualityIssueNegativeVolume, QualitySeverityError) {
		t.Errorf("issues = %#v, want negative volume error", issues)
	}
}

// TestQualityTimestamp 验证重复或乱序的时间戳会生成错误问题。
func TestQualityTimestamp(t *testing.T) {
	baseTime := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	bars := []datasource.Bar{
		validQualityBar(baseTime, 10),
		validQualityBar(baseTime, 11),
		validQualityBar(baseTime.Add(-time.Hour), 12),
	}

	issues := ValidateMarketData(bars, nil, nil)
	if countQualityIssues(issues, QualityIssueTimestampOrder, QualitySeverityError) != 2 {
		t.Errorf("issues = %#v, want two timestamp-order errors", issues)
	}
}

// TestQualityMissingTradingDays 验证超过五个开市日的缺口会生成警告。
func TestQualityMissingTradingDays(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	calendar := make([]datasource.TradingDay, 0, 8)
	for offset := range 8 {
		calendar = append(calendar, datasource.TradingDay{
			Date:         start.AddDate(0, 0, offset),
			IsTradingDay: true,
		})
	}
	bars := []datasource.Bar{
		validQualityBar(start, 10),
		validQualityBar(start.AddDate(0, 0, 7), 10),
	}

	issues := ValidateMarketData(bars, nil, calendar)
	if !containsQualityIssue(issues, QualityIssueMissingTradingDays, QualitySeverityWarning) {
		t.Errorf("issues = %#v, want missing trading days warning", issues)
	}
}

// TestQualitySplitRatio 验证不合理的拆股前后价格比例会生成警告。
func TestQualitySplitRatio(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := []datasource.Bar{
		validQualityBar(start, 100),
		validQualityBar(start.AddDate(0, 0, 1), 40),
	}
	actions := []datasource.CorporateAction{{
		Type:       datasource.ActionSplit,
		Date:       start.AddDate(0, 0, 1),
		SplitRatio: 2,
	}}

	issues := ValidateMarketData(bars, actions, nil)
	if !containsQualityIssue(issues, QualityIssueSplitPriceMismatch, QualitySeverityWarning) {
		t.Errorf("issues = %#v, want split price mismatch warning", issues)
	}
}

// TestQualityAbnormalMove 验证超过百分之五十的普通日间波动会生成警告。
func TestQualityAbnormalMove(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := []datasource.Bar{
		validQualityBar(start, 100),
		validQualityBar(start.AddDate(0, 0, 1), 160),
	}

	issues := ValidateMarketData(bars, nil, nil)
	if !containsQualityIssue(issues, QualityIssueAbnormalPriceMove, QualitySeverityWarning) {
		t.Errorf("issues = %#v, want abnormal price move warning", issues)
	}
}

// TestQualityAbnormalMoveOnSplit 验证拆股日会抑制预期的大幅价格波动警告。
func TestQualityAbnormalMoveOnSplit(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := []datasource.Bar{
		validQualityBar(start, 100),
		validQualityBar(start.AddDate(0, 0, 1), 160),
	}
	actions := []datasource.CorporateAction{{
		Type:       datasource.ActionReverseSplit,
		Date:       start.AddDate(0, 0, 1),
		SplitRatio: 0.625,
	}}

	issues := ValidateMarketData(bars, actions, nil)
	if containsQualityIssue(issues, QualityIssueAbnormalPriceMove, QualitySeverityWarning) {
		t.Errorf("issues = %#v, do not want abnormal price move warning on a split date", issues)
	}
}

// validQualityBar 创建能通过单项价格校验的日线数据。
func validQualityBar(timestamp time.Time, close float64) datasource.Bar {
	return datasource.Bar{
		Time:   timestamp,
		Open:   close,
		High:   close + 1,
		Low:    close - 1,
		Close:  close,
		Volume: 100,
	}
}

// containsQualityIssue 判断问题列表是否包含指定代码和严重级别。
func containsQualityIssue(issues []DataQualityIssue, code QualityIssueCode, severity QualitySeverity) bool {
	return countQualityIssues(issues, code, severity) > 0
}

// countQualityIssues 统计匹配指定代码和严重级别的问题数量。
func countQualityIssues(issues []DataQualityIssue, code QualityIssueCode, severity QualitySeverity) int {
	count := 0
	for _, issue := range issues {
		if issue.Code == code && issue.Severity == severity {
			count++
		}
	}
	return count
}
