package datasource

import (
	"context"
	"iter"
)

type HistoricalSource interface {
	Bars() iter.Seq2[Bar, error]
	CorporateActions() // 公司行为
}

type RealtimeSource interface {
	Subscribe(ctx context.Context, symbols []string) (<-chan Bar, error) // 实现内部负责重连与断档上报
}

type MetadataSource interface {
	Symbols() (SymbolInfo, error)
	Calendar() ([]TradingDay, error)
}
