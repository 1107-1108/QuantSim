package datasource

import (
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

var yahoo_url = "https://query1.finance.yahoo.com/v8/finance/chart/"
var yahooHTTPClient = &http.Client{Timeout: yahooRequestTimeout}

const yahooRequestTimeout = 15 * time.Second

// TradingPeriod 单个交易时段
type TradingPeriod struct {
	Start     int64  `json:"start"`
	End       int64  `json:"end"`
	Timezone  string `json:"timezone"`
	GMTOffset int64  `json:"gmtoffset"`
}

type CurrentTradingPeriod struct {
	Pre     TradingPeriod `json:"pre"`
	Regular TradingPeriod `json:"regular"`
	Post    TradingPeriod `json:"post"`
}

type YahooResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				// 股票基本信息
				Symbol           string `json:"symbol"`
				Currency         string `json:"currency"`
				ExchangeName     string `json:"exchangeName"`
				FullExchangeName string `json:"fullExchangeName"`
				AssetType        string `json:"instrumentType"`
				LongName         string `json:"longName"`
				ShortName        string `json:"shortName"`
				FirstTradeDate   int64  `json:"firstTradeDate"`

				// 时区信息
				TimezoneName string `json:"exchangeTimezoneName"`
				Timezone     string `json:"timezone"`
				GMTOffset    int64  `json:"gmtoffset"`

				// 行情基本信息
				RegularMarketPrice         float64 `json:"regularMarketPrice"`
				RegularMarketTime          int64   `json:"regularMarketTime"`
				RegularMarketChangePercent float64 `json:"regularMarketChangePercent"`
				RegularMarketDayHigh       float64 `json:"regularMarketDayHigh"`
				RegularMarketDayLow        float64 `json:"regularMarketDayLow"`
				RegularMarketVolume        int64   `json:"regularMarketVolume"`

				PreviousClose      float64 `json:"previousClose"`
				ChartPreviousClose float64 `json:"chartPreviousClose"`

				FiftyTwoWeekHigh float64 `json:"fiftyTwoWeekHigh"`
				FiftyTwoWeekLow  float64 `json:"fiftyTwoWeekLow"`

				FullDayPrice         float64 `json:"fulldayPrice"`
				FullDayChange        float64 `json:"fulldayChange"`
				FullDayChangePercent float64 `json:"fulldayChangePercent"`

				HasPrePostMarketData bool `json:"hasPrePostMarketData"`
				PriceHint            int  `json:"priceHint"`
				Scale                int  `json:"scale"`

				// 数据范围
				DataGranularity string   `json:"dataGranularity"`
				Range           string   `json:"range"`
				ValidRanges     []string `json:"validRanges"`

				// 当前交易日的交易时段
				CurrentTradingPeriod CurrentTradingPeriod `json:"currentTradingPeriod"`

				// 查询范围内的历史交易时段
				TradingPeriods [][]TradingPeriod `json:"tradingPeriods"`
			} `json:"meta"`

			Event struct {
				Dividends map[string]struct {
					Amount float64 `json:"amount"`
					Date   int64   `json:"date"`
				} `json:"dividends"`

				Splits map[string]struct {
					Date        int64   `json:"date"`
					Numerator   float64 `json:"numerator"`
					Denominator float64 `json:"denominator"`
					SplitRatio  string  `json:"splitRatio"`
				} `json:"splits"`
			} `json:"events"`

			// 每根 K 线对应的 Unix 时间戳
			Timestamp []int64 `json:"timestamp"`

			Indicators struct {
				Quote []struct {
					Open   []float64 `json:"open"`
					High   []float64 `json:"high"`
					Low    []float64 `json:"low"`
					Close  []float64 `json:"close"`
					Volume []int64   `json:"volume"`
				} `json:"quote"`

				AdjClose []struct {
					Values []float64 `json:"adjclose"`
				} `json:"adjclose"`
			} `json:"indicators"`
		} `json:"result"`

		Error json.RawMessage `json:"error"`
	} `json:"chart"`
}

// YahooApiMaker 根据股票代码和起止日期构造 Yahoo Finance 日线行情查询地址。
// start 和 end 必须使用 YYYY-MM-DD 格式；end 表示不包含在结果中的结束日期。
func YahooApiMaker(symbol, start, end, adjClose, interval string) (string, error) {
	if strings.TrimSpace(symbol) == "" {
		return "", fmt.Errorf("股票代码不能为空")
	}

	layout := "2006-01-02"
	parsedStart, err := time.Parse(layout, start)
	if err != nil {
		return "", fmt.Errorf("解析开始日期 %q: %w", start, err)
	}
	parsedEnd, err := time.Parse(layout, end)
	if err != nil {
		return "", fmt.Errorf("解析结束日期 %q: %w", end, err)
	}
	if !parsedStart.Before(parsedEnd) {
		return "", fmt.Errorf("开始日期 %s 必须早于结束日期 %s", start, end)
	}

	return yahoo_url + url.PathEscape(symbol) + "?period1=" +
		strconv.FormatInt(parsedStart.Unix(), 10) +
		"&period2=" + strconv.FormatInt(parsedEnd.Unix(), 10) +
		"&interval=" + interval + "&events=div,split&includeAdjustedClose=" + adjClose, nil
}

// GetResponseWithClient 使用调用方提供的 HTTP 客户端请求 Yahoo 数据。
func (res *YahooResponse) GetResponseWithClient(client *http.Client, url string) (YahooResponse, error) {
	if client == nil {
		return YahooResponse{}, fmt.Errorf("yahoo HTTP 客户端不能为空")
	}

	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return YahooResponse{}, fmt.Errorf("创建 Yahoo 请求: %w", err)
	}
	request.Header.Set("User-Agent", "Mozilla/5.0")
	request.Header.Add("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return YahooResponse{}, fmt.Errorf("请求 Yahoo 数据: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return YahooResponse{}, fmt.Errorf("yahoo HTTP 状态错误: %s", response.Status)
	}

	var data YahooResponse
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
		return YahooResponse{}, fmt.Errorf("解析 Yahoo JSON: %w", err)
	}
	if res != nil {
		*res = data
	}
	return data, nil
}

// GetResponse 向指定地址发送HTTP GET请求，并将返回的JSON解析为YahooResponse
func (res *YahooResponse) GetResponse(url string) (YahooResponse, error) {
	return res.GetResponseWithClient(yahooHTTPClient, url)
}

func ValidateYahooResponse(data YahooResponse) error {
	if len(data.Chart.Result) == 0 {
		return fmt.Errorf("yahoo响应缺少result")
	}
	result := data.Chart.Result[0]
	if len(result.Indicators.Quote) == 0 {
		return fmt.Errorf("yahoo 响应缺少 quote")
	}
	if len(result.Indicators.AdjClose) == 0 {
		return fmt.Errorf("yahoo 响应缺少 adjclose")
	}

	want := len(result.Timestamp)
	quote := result.Indicators.Quote[0]
	adjClose := result.Indicators.AdjClose[0]
	lengths := []struct {
		name string
		got  int
	}{
		{name: "open", got: len(quote.Open)},
		{name: "high", got: len(quote.High)},
		{name: "low", got: len(quote.Low)},
		{name: "close", got: len(quote.Close)},
		{name: "volume", got: len(quote.Volume)},
		{name: "adjclose", got: len(adjClose.Values)},
	}

	for _, field := range lengths {
		if field.got != want {
			return fmt.Errorf("%s 数组长度为 %d，时间戳数组长度为 %d", field.name, field.got, want)
		}
	}
	return nil

}

// YahooResponse2Bars 把 Yahoo 响应转换为 K 线切片。
// 响应校验失败时返回错误，调用方可以区分"无数据"与"数据损坏"。
func YahooResponse2Bars(response YahooResponse) ([]Bar, error) {
	if err := ValidateYahooResponse(response); err != nil {
		return nil, fmt.Errorf("转换 Yahoo 响应为 K 线: %w", err)
	}

	result := response.Chart.Result[0]
	quote := result.Indicators.Quote[0]
	adjClose := result.Indicators.AdjClose[0]

	bars := make([]Bar, 0, len(result.Timestamp))
	for i, timestamp := range result.Timestamp {
		bars = append(bars, Bar{
			Time:     time.Unix(timestamp, 0).UTC(),
			Open:     quote.Open[i],
			High:     quote.High[i],
			Low:      quote.Low[i],
			Close:    quote.Close[i],
			AdjClose: adjClose.Values[i],
			Volume:   quote.Volume[i],
		})
	}
	return bars, nil
}

// YahooResponse2TradingDays 把 Yahoo 响应中的交易时段转换为交易日历。
// Date 按交易所时区从首个时段的开市时间推导，避免 UTC 换算导致的日期偏移。
func YahooResponse2TradingDays(response YahooResponse) ([]TradingDay, error) {
	if len(response.Chart.Result) == 0 {
		return nil, fmt.Errorf("yahoo 响应缺少 result")
	}

	result := response.Chart.Result[0]
	location := yahooExchangeLocation(result.Meta.TimezoneName, result.Meta.Timezone, result.Meta.GMTOffset)

	tradingDays := make([]TradingDay, 0, len(result.Meta.TradingPeriods))
	for _, periods := range result.Meta.TradingPeriods {
		if len(periods) == 0 {
			continue
		}
		sessions := make([]TradeSession, 0, len(periods))
		for _, period := range periods {
			sessions = append(sessions, TradeSession{
				Open:  time.Unix(period.Start, 0).In(location),
				Close: time.Unix(period.End, 0).In(location),
			})
		}
		marketOpen := sessions[0].Open
		tradingDays = append(tradingDays, TradingDay{
			Date:         time.Date(marketOpen.Year(), marketOpen.Month(), marketOpen.Day(), 0, 0, 0, 0, location),
			Exchange:     result.Meta.ExchangeName,
			IsTradingDay: true,
			Sessions:     sessions,
		})
	}
	return tradingDays, nil
}

func YahooResponse2CorporateActions(response YahooResponse) ([]CorporateAction, error) {
	if len(response.Chart.Result) == 0 {
		return nil, fmt.Errorf("yahoo 响应缺少result")
	}
	result := response.Chart.Result[0]
	event := result.Event
	actions := make([]CorporateAction, 0, len(event.Dividends)+len(event.Splits))
	for _, action := range event.Dividends {
		actionName := ActionDividend

		temp := CorporateAction{
			Symbol:       result.Meta.Symbol,
			Type:         CorporateActionType(actionName),
			Date:         time.Unix(action.Date, 0).UTC(),
			CashPerShare: action.Amount,
			SplitRatio:   0,
		}
		actions = append(actions, temp)
	}

	for _, action := range event.Splits {
		if action.Denominator == 0 {
			return nil, fmt.Errorf("yahoo 拆股数据异常: %s 的 denominator 为 0", result.Meta.Symbol)
		}
		actionName := ActionSplit
		if action.Numerator < action.Denominator {
			actionName = ActionReverseSplit
		}

		temp := CorporateAction{
			Symbol:       result.Meta.Symbol,
			Type:         CorporateActionType(actionName),
			Date:         time.Unix(action.Date, 0).UTC(),
			CashPerShare: 0,
			SplitRatio:   action.Numerator / action.Denominator,
		}
		actions = append(actions, temp)
	}

	slices.SortFunc(actions, func(a, b CorporateAction) int {
		return a.Date.Compare(b.Date)
	})

	return actions, nil
}

// yahooExchangeLocation 优先使用 IANA 时区名（能正确处理历史夏令时）；
// 名称不可用时回退到按 GMT 偏移构造的固定时区。
func yahooExchangeLocation(timezoneName, timezoneAbbrev string, gmtOffset int64) *time.Location {
	if location, err := time.LoadLocation(timezoneName); err == nil {
		return location
	}
	return time.FixedZone(timezoneAbbrev, int(gmtOffset))
}

// Bars 是HistoricalSource interface定义的函数
// 获取提供时间段里的K线组合
func (y *YahooResponse) Bars() iter.Seq2[Bar, error] {
	return func(yield func(Bar, error) bool) {
		bars, err := YahooResponse2Bars(*y)
		if err != nil {
			yield(Bar{}, err)
			return
		}
		for _, bar := range bars {
			if !yield(bar, nil) {
				return
			}
		}

	}

}

// CorporateActions 是HistoricalSource interface定义的函数
// 获取公司行为的信息
func (y *YahooResponse) CorporateActions() ([]CorporateAction, error) {
	actions, err := YahooResponse2CorporateActions(*y)
	if err != nil {
		return nil, err
	}
	return actions, err
}

func (y *YahooResponse) Symbols() (SymbolInfo, error) {
	result := y.Chart.Result[0]
	meta := result.Meta
	var assetType AssetType
	if meta.AssetType == "EQUITY" {
		assetType = AssetStock
	}
	// TODO ETF

	return SymbolInfo{
		Symbol:     meta.Symbol,
		Name:       meta.LongName,
		Exchange:   meta.ExchangeName,
		Currency:   meta.Currency,
		AssetType:  assetType,
		ListDate:   time.Unix(meta.FirstTradeDate, 0).UTC(),
		DelistDate: nil,
		Timezone:   meta.Timezone,
		LotSize:    -1,
		TickSize:   -1,
		ISIN:       "",
	}, nil
}

// Calendar 生成交易日日历
func (y *YahooResponse) Calendar() ([]TradingDay, error) {
	result := y.Chart.Result[0]
	timestamps := y.Chart.Result[0].Timestamp
	tradingDays := make([]TradingDay, 0, len(timestamps))
	for _, timestamp := range timestamps {
		temp := TradingDay{
			Date:         time.Unix(timestamp, 0).UTC(),
			Exchange:     result.Meta.ExchangeName,
			IsTradingDay: true,
			Sessions:     nil,
			HolidayName:  "",
		}
		tradingDays = append(tradingDays, temp)
	}
	return tradingDays, nil
}
