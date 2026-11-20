package datasource

import (
	"fmt"
	"math"
	"time"
)

// Bar 是K线的基本单位
type Bar struct {
	Time       time.Time
	Open       float64
	High       float64
	Low        float64
	Close      float64
	Volume     int64
	AdjClose   float64    // Provider's adjusted close, or zero when unavailable.
	PriceBasis PriceBasis `parquet:",optional"` // Price basis of OHLC.
}

// PriceBasis distinguishes trade-time OHLC from provider-adjusted and legacy prices.
type PriceBasis int32

const (
	PriceAsTraded PriceBasis = iota
	PriceSplitAdjusted
	PriceBasisUnknown
)

// AsTraded restores OHLC using the product of the new-to-old share ratios
// for all splits reflected in these prices that took effect after this bar.
// Use 1 if no such splits occurred. The factor must cover the provider's
// adjustment horizon, even if it extends past the requested history. A bar
// that straddles a split must be reconstructed from smaller bars first.
// AdjClose and Volume retain the provider's values.
func (bar Bar) AsTraded(splitFactor float64) (Bar, error) {
	if bar.PriceBasis != PriceSplitAdjusted {
		return Bar{}, fmt.Errorf("bar OHLC must have a known split-adjusted basis")
	}
	if !positiveFinite(splitFactor) {
		return Bar{}, fmt.Errorf("invalid split factor: %v", splitFactor)
	}
	bar.Open *= splitFactor
	bar.High *= splitFactor
	bar.Low *= splitFactor
	bar.Close *= splitFactor
	for _, price := range [...]float64{bar.Open, bar.High, bar.Low, bar.Close} {
		if !positiveFinite(price) {
			return Bar{}, fmt.Errorf("invalid as-traded price: %v", price)
		}
	}
	bar.PriceBasis = PriceAsTraded
	return bar, nil
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsInf(value, 0)
}

// Timeframe 表示K线周期，比如1m，5m，1h，1d，1wk
type Timeframe string

const (
	TF1m  Timeframe = "1m"
	TF5m  Timeframe = "5m"
	TF1d  Timeframe = "1d"
	TF1wk Timeframe = "1wk"
)

// CorporateActionType 定义了公司行为，比如现金分红，股票拆分，反向拆分等
type CorporateActionType string

const (
	ActionDividend     CorporateActionType = "DIVIDEND"      // 现金分红
	ActionSplit        CorporateActionType = "SPLIT"         // 股票拆分
	ActionReverseSplit CorporateActionType = "REVERSE_SPLIT" //反向拆分
)

// TODO 拆股后payday没做
type CorporateAction struct {
	Symbol string
	Type   CorporateActionType
	Date   time.Time // 除息日 for 拆股

	CashPerShare float64 // 每股现金分红
	SplitRatio   float64 // 拆股比例
}

// AssetType 资产类型，比如股票，ETF，期货等等，现在只支持股票和ETF
type AssetType string

const (
	AssetStock AssetType = "STOCK"
	AssetETF   AssetType = "ETF"
)

type SymbolInfo struct {
	Symbol     string    // "AAPL"
	Name       string    // "Apple Inc."
	Exchange   string    // "NASDAQ"
	Currency   string    // "USD"
	AssetType  AssetType // "STOCK"
	ListDate   time.Time // 上市日期
	DelistDate *time.Time
	Timezone   string  // "America/New_York"
	LotSize    int64   //最小交易数量单位, -1代表不提供
	TickSize   float64 // 最小价格变动单位, -1代表不提供
	ISIN       string  // 国际证券识别码
}

type TradingDay struct {
	Date         time.Time
	Exchange     string
	IsTradingDay bool
	Sessions     []TradeSession // 当天的交易时间段
	HolidayName  string
}

type TradeSession struct {
	Open  time.Time
	Close time.Time
}
