package datasource

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// R2:interval 必须命中白名单,防止调用方经 query 拼接注入额外参数
func TestYahooApiMaker_RejectsUnsupportedInterval(t *testing.T) {
	if _, err := YahooApiMaker("AAPL", "2024-01-02", "2024-01-10", "true", "1d&injected=1"); err == nil {
		t.Fatal("want error for interval containing injected query params")
	}
	if _, err := YahooApiMaker("AAPL", "2024-01-02", "2024-01-10", "true", "1mo"); err == nil {
		t.Fatal("want error for interval outside platform whitelist")
	}
}

// R2:adjClose 只允许 "true"/"false"
func TestYahooApiMaker_RejectsInvalidAdjClose(t *testing.T) {
	if _, err := YahooApiMaker("AAPL", "2024-01-02", "2024-01-10", "maybe", "1d"); err == nil {
		t.Fatal("want error for invalid adjClose")
	}
}

// R2:白名单内的时间帧全部放行(与 data.go 的 Timeframe 常量对齐)
func TestYahooApiMaker_AcceptsWhitelistedIntervals(t *testing.T) {
	for _, interval := range []string{"1m", "5m", "1d", "1wk"} {
		if _, err := YahooApiMaker("AAPL", "2024-01-02", "2024-01-10", "true", interval); err != nil {
			t.Errorf("interval %q: unexpected error %v", interval, err)
		}
	}
}

// R3:空 result(Yahoo 对无效 symbol 的响应形态)必须返回错误,不得 panic
func TestYahooResponse_SymbolsEmptyResultReturnsError(t *testing.T) {
	var response YahooResponse
	if _, err := response.Symbols(); err == nil {
		t.Fatal("want error for empty result, not panic")
	}
}

func TestYahooResponse_CalendarEmptyResultReturnsError(t *testing.T) {
	var response YahooResponse
	if _, err := response.Calendar(); err == nil {
		t.Fatal("want error for empty result, not panic")
	}
}

// R3:Yahoo 错误响应体(chart.error 非空)必须显式上抛,而非当作成功返回
func TestYahooResponse_GetResponseWithClientSurfacesChartError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chart":{"result":null,"error":{"code":"Not Found","description":"No data found, symbol may be delisted"}}}`))
	}))
	defer server.Close()

	var response YahooResponse
	_, err := response.GetResponseWithClient(server.Client(), server.URL)
	if err == nil {
		t.Fatal("want chart error surfaced as Go error")
	}
	if !strings.Contains(err.Error(), "yahoo") {
		t.Errorf("error = %v, want it to identify Yahoo as the source", err)
	}
}
