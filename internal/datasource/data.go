package datasource

import "time"

// Bar 是K线的基本单位
type Bar struct {
	Time     time.Time
	Open     float64
	High     float64
	Low      float64
	Close    float64
	Volume   int64
	AdjClose float64
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
