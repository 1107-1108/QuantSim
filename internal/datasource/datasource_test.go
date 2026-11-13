package datasource

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestYahooApiMaker(t *testing.T) {
	t.Run("builds URL with unix timestamps for a valid date range", func(t *testing.T) {
		// Arrange
		want := "https://query1.finance.yahoo.com/v8/finance/chart/AAPL" +
			"?period1=1704153600&period2=1704844800" +
			"&interval=1d&events=div,split&includeAdjustedClose=true"

		// Act
		got, err := YahooApiMaker("AAPL", "2024-01-02", "2024-01-10", "true", "1d")

		// Assert
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got != want {
			t.Errorf("URL mismatch\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("escapes special characters in the symbol", func(t *testing.T) {
		// Arrange
		wantPathSuffix := "/%5EGSPC"

		// Act
		got, err := YahooApiMaker("^GSPC", "2024-01-02", "2024-01-10", "true", "1d")

		// Assert
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !strings.Contains(got, wantPathSuffix) {
			t.Errorf("expected symbol to be path-escaped as %q, got URL %q", wantPathSuffix, got)
		}
	})

	t.Run("passes the adjClose flag through to the query", func(t *testing.T) {
		// Act
		got, err := YahooApiMaker("AAPL", "2024-01-02", "2024-01-10", "false", "1d")

		// Assert
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !strings.HasSuffix(got, "includeAdjustedClose=false") {
			t.Errorf("expected includeAdjustedClose=false, got URL %q", got)
		}
	})

	t.Run("produces a parseable URL with the expected query parameters", func(t *testing.T) {
		// Act
		raw, err := YahooApiMaker("MSFT", "2024-01-02", "2024-01-10", "true", "1d")

		// Assert
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("generated URL does not parse: %v", err)
		}
		if parsed.Scheme != "https" {
			t.Errorf("expected scheme https, got %q", parsed.Scheme)
		}
		if parsed.Host != "query1.finance.yahoo.com" {
			t.Errorf("unexpected host %q", parsed.Host)
		}
		if parsed.Path != "/v8/finance/chart/MSFT" {
			t.Errorf("unexpected path %q", parsed.Path)
		}
		wantQuery := map[string]string{
			"period1":              "1704153600",
			"period2":              "1704844800",
			"interval":             "1d",
			"events":               "div,split",
			"includeAdjustedClose": "true",
		}
		query := parsed.Query()
		for key, want := range wantQuery {
			if got := query.Get(key); got != want {
				t.Errorf("query param %s: got %q, want %q", key, got, want)
			}
		}
	})

	t.Run("rejects invalid input", func(t *testing.T) {
		cases := []struct {
			name        string
			symbol      string
			start       string
			end         string
			wantErrPart string
		}{
			{"empty symbol", "", "2024-01-02", "2024-01-10", "symbol cannot be empty"},
			{"whitespace-only symbol", "   ", "2024-01-02", "2024-01-10", "symbol cannot be empty"},
			{"start date with wrong format", "AAPL", "2024/01/02", "2024-01-10", "parse start date"},
			{"nonexistent start date", "AAPL", "2024-13-01", "2024-01-10", "parse start date"},
			{"end date with wrong format", "AAPL", "2024-01-02", "not-a-date", "parse end date"},
			{"start date equal to end date", "AAPL", "2024-01-10", "2024-01-10", "must be before"},
			{"start date after end date", "AAPL", "2024-01-10", "2024-01-02", "must be before"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				// Act
				got, err := YahooApiMaker(tc.symbol, tc.start, tc.end, "true", "1d")

				// Assert
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (URL: %s)", tc.wantErrPart, got)
				}
				if got != "" {
					t.Errorf("expected empty URL on error, got %q", got)
				}
				if !strings.Contains(err.Error(), tc.wantErrPart) {
					t.Errorf("expected error containing %q, got %q", tc.wantErrPart, err.Error())
				}
			})
		}
	})
}

func decodeYahooResponse(t *testing.T, payload string) YahooResponse {
	t.Helper()
	var response YahooResponse
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		t.Fatalf("decode fixture JSON: %v", err)
	}
	return response
}

func TestYahooResponse2Bars(t *testing.T) {
	t.Run("converts timestamps and quotes into bars", func(t *testing.T) {
		// Arrange
		payload := `{
			"chart": {
				"result": [{
					"meta": {"symbol": "AAPL"},
					"timestamp": [1704153600, 1704240000],
					"indicators": {
						"quote": [{
							"open": [150.5, 151.0],
							"high": [151.0, 152.0],
							"low": [150.0, 150.5],
							"close": [150.8, 151.8],
							"volume": [1000, 2000]
						}],
						"adjclose": [{"adjclose": [150.1, 151.1]}]
					}
				}],
				"error": null
			}
		}`
		response := decodeYahooResponse(t, payload)

		// Act
		bars, err := YahooResponse2Bars(response)

		// Assert
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(bars) != 2 {
			t.Fatalf("expected 2 bars, got %d", len(bars))
		}
		first := bars[0]
		if !first.Time.Equal(time.Unix(1704153600, 0).UTC()) {
			t.Errorf("Time: got %v, want %v", first.Time, time.Unix(1704153600, 0).UTC())
		}
		if first.Open != 150.5 || first.High != 151.0 || first.Low != 150.0 || first.Close != 150.8 {
			t.Errorf("OHLC mismatch: got %+v", first)
		}
		if first.Volume != 1000 {
			t.Errorf("Volume: got %d, want 1000", first.Volume)
		}
		if first.AdjClose != 150.1 {
			t.Errorf("AdjClose: got %f, want 150.1", first.AdjClose)
		}
		if bars[1].Close != 151.8 {
			t.Errorf("second bar Close: got %f, want 151.8", bars[1].Close)
		}
	})

	t.Run("returns error when result is missing", func(t *testing.T) {
		// Arrange
		response := decodeYahooResponse(t, `{"chart": {"result": [], "error": null}}`)

		// Act
		bars, err := YahooResponse2Bars(response)

		// Assert
		if err == nil {
			t.Fatal("expected error for missing result, got nil")
		}
		if bars != nil {
			t.Errorf("expected nil bars on error, got %v", bars)
		}
	})

	t.Run("returns error when quote lengths mismatch timestamps", func(t *testing.T) {
		// Arrange
		payload := `{
			"chart": {
				"result": [{
					"meta": {"symbol": "AAPL"},
					"timestamp": [1704153600, 1704240000],
					"indicators": {
						"quote": [{
							"open": [150.5, 151.0],
							"high": [151.0, 152.0],
							"low": [150.0, 150.5],
							"close": [150.8],
							"volume": [1000, 2000]
						}],
						"adjclose": [{"adjclose": [150.1, 151.1]}]
					}
				}],
				"error": null
			}
		}`
		response := decodeYahooResponse(t, payload)

		// Act
		_, err := YahooResponse2Bars(response)

		// Assert
		if err == nil {
			t.Fatal("expected error for length mismatch, got nil")
		}
		if !strings.Contains(err.Error(), "close") {
			t.Errorf("expected error to name the close field, got %q", err.Error())
		}
	})
}

func TestYahooResponse2TradingDays(t *testing.T) {
	t.Run("derives trading date in exchange timezone rather than UTC", func(t *testing.T) {
		// Arrange：悉尼夏令时 UTC+11，当地 10:00 开市对应 UTC 前一天 23:00。
		// 如果实现按 UTC 取日期，会得到错误的交易日。
		sydney, err := time.LoadLocation("Australia/Sydney")
		if err != nil {
			t.Fatalf("load time zone: %v", err)
		}
		day1Open := time.Date(2024, 1, 2, 10, 0, 0, 0, sydney)
		day1Close := time.Date(2024, 1, 2, 16, 0, 0, 0, sydney)
		day2Open := time.Date(2024, 1, 3, 10, 0, 0, 0, sydney)
		day2Close := time.Date(2024, 1, 3, 16, 0, 0, 0, sydney)
		payload := fmt.Sprintf(`{
			"chart": {
				"result": [{
					"meta": {
						"exchangeName": "ASX",
						"exchangeTimezoneName": "Australia/Sydney",
						"timezone": "AEDT",
						"gmtoffset": 39600,
						"tradingPeriods": [
							[{"start": %d, "end": %d, "timezone": "AEDT", "gmtoffset": 39600}],
							[{"start": %d, "end": %d, "timezone": "AEDT", "gmtoffset": 39600}]
						]
					}
				}],
				"error": null
			}
		}`, day1Open.Unix(), day1Close.Unix(), day2Open.Unix(), day2Close.Unix())
		response := decodeYahooResponse(t, payload)

		// Act
		days, err := YahooResponse2TradingDays(response)

		// Assert
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(days) != 2 {
			t.Fatalf("expected 2 trading days, got %d", len(days))
		}
		first := days[0]
		if first.Date.Year() != 2024 || first.Date.Month() != time.January || first.Date.Day() != 2 {
			t.Errorf("Date: got %v, want 2024-01-02 (exchange-local, not UTC)", first.Date)
		}
		if _, offset := first.Date.Zone(); offset != 39600 {
			t.Errorf("Date zone offset: got %d, want 39600 (Australia/Sydney)", offset)
		}
		if !first.IsTradingDay {
			t.Error("expected IsTradingDay to be true")
		}
		if first.Exchange != "ASX" {
			t.Errorf("Exchange: got %q, want ASX", first.Exchange)
		}
		if len(first.Sessions) != 1 {
			t.Fatalf("expected 1 session, got %d", len(first.Sessions))
		}
		if !first.Sessions[0].Open.Equal(day1Open) || !first.Sessions[0].Close.Equal(day1Close) {
			t.Errorf("session mismatch: got %+v, want %v ~ %v", first.Sessions[0], day1Open, day1Close)
		}
		if first.Sessions[0].Open.Hour() != 10 {
			t.Errorf("session open hour: got %d, want 10 (exchange-local)", first.Sessions[0].Open.Hour())
		}
		if days[1].Date.Day() != 3 {
			t.Errorf("second day Date: got %v, want 2024-01-03", days[1].Date)
		}
	})

	t.Run("keeps multiple sessions of one day on the same trading day", func(t *testing.T) {
		// Arrange：东京交易所午间休市，一天有两个时段。
		tokyo, err := time.LoadLocation("Asia/Tokyo")
		if err != nil {
			t.Fatalf("load time zone: %v", err)
		}
		morningOpen := time.Date(2024, 1, 2, 9, 0, 0, 0, tokyo)
		morningClose := time.Date(2024, 1, 2, 11, 30, 0, 0, tokyo)
		afternoonOpen := time.Date(2024, 1, 2, 12, 30, 0, 0, tokyo)
		afternoonClose := time.Date(2024, 1, 2, 15, 30, 0, 0, tokyo)
		payload := fmt.Sprintf(`{
			"chart": {
				"result": [{
					"meta": {
						"exchangeName": "TYO",
						"exchangeTimezoneName": "Asia/Tokyo",
						"timezone": "JST",
						"gmtoffset": 32400,
						"tradingPeriods": [
							[
								{"start": %d, "end": %d, "timezone": "JST", "gmtoffset": 32400},
								{"start": %d, "end": %d, "timezone": "JST", "gmtoffset": 32400}
							]
						]
					}
				}],
				"error": null
			}
		}`, morningOpen.Unix(), morningClose.Unix(), afternoonOpen.Unix(), afternoonClose.Unix())
		response := decodeYahooResponse(t, payload)

		// Act
		days, err := YahooResponse2TradingDays(response)

		// Assert
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(days) != 1 {
			t.Fatalf("expected 1 trading day, got %d", len(days))
		}
		day := days[0]
		if len(day.Sessions) != 2 {
			t.Fatalf("expected 2 sessions, got %d", len(day.Sessions))
		}
		if !day.Sessions[0].Open.Equal(morningOpen) || !day.Sessions[1].Open.Equal(afternoonOpen) {
			t.Errorf("sessions not preserved in order: got %+v", day.Sessions)
		}
		if day.Date.Day() != 2 {
			t.Errorf("Date: got %v, want 2024-01-02", day.Date)
		}
	})

	t.Run("falls back to GMT offset when timezone name is unknown", func(t *testing.T) {
		// Arrange
		open := time.Date(2024, 1, 2, 8, 0, 0, 0, time.UTC)
		close_ := time.Date(2024, 1, 2, 16, 0, 0, 0, time.UTC)
		payload := fmt.Sprintf(`{
			"chart": {
				"result": [{
					"meta": {
						"exchangeName": "XXX",
						"exchangeTimezoneName": "Bogus/Zone",
						"timezone": "CET",
						"gmtoffset": 3600,
						"tradingPeriods": [
							[{"start": %d, "end": %d, "timezone": "CET", "gmtoffset": 3600}]
						]
					}
				}],
				"error": null
			}
		}`, open.Unix(), close_.Unix())
		response := decodeYahooResponse(t, payload)

		// Act
		days, err := YahooResponse2TradingDays(response)

		// Assert
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(days) != 1 {
			t.Fatalf("expected 1 trading day, got %d", len(days))
		}
		if days[0].Sessions[0].Open.Hour() != 9 {
			t.Errorf("session open hour: got %d, want 9 (UTC+1 fallback)", days[0].Sessions[0].Open.Hour())
		}
		if _, offset := days[0].Date.Zone(); offset != 3600 {
			t.Errorf("Date zone offset: got %d, want 3600", offset)
		}
	})

	t.Run("returns error when result is missing", func(t *testing.T) {
		// Arrange
		response := decodeYahooResponse(t, `{"chart": {"result": [], "error": null}}`)

		// Act
		_, err := YahooResponse2TradingDays(response)

		// Assert
		if err == nil {
			t.Fatal("expected error for missing result, got nil")
		}
	})
}

func TestGetResponse(t *testing.T) {
	t.Run("fetches and decodes a valid chart response", func(t *testing.T) {
		// Arrange
		var gotUserAgent string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUserAgent = r.Header.Get("User-Agent")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{
				"chart": {
					"result": [{
						"meta": {"symbol": "AAPL", "exchangeName": "NMS"},
						"timestamp": [1704153600],
						"indicators": {
							"quote": [{"open": [150.5], "high": [151.0], "low": [150.0], "close": [150.8], "volume": [1000]}],
							"adjclose": [{"adjclose": [150.1]}]
						}
					}],
					"error": null
				}
			}`)
		}))
		defer server.Close()

		// Act
		var response YahooResponse
		data, err := response.GetResponse(server.URL)

		// Assert
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(data.Chart.Result) != 1 {
			t.Fatalf("expected 1 result, got %d", len(data.Chart.Result))
		}
		if data.Chart.Result[0].Meta.Symbol != "AAPL" {
			t.Errorf("Symbol: got %q, want AAPL", data.Chart.Result[0].Meta.Symbol)
		}
		if gotUserAgent != "Mozilla/5.0" {
			t.Errorf("User-Agent: got %q, want Mozilla/5.0", gotUserAgent)
		}
	})

	t.Run("returns error for non-200 status", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		}))
		defer server.Close()

		// Act
		var response YahooResponse
		_, err := response.GetResponse(server.URL)

		// Assert
		if err == nil {
			t.Fatal("expected error for non-200 status, got nil")
		}
		if !strings.Contains(err.Error(), "429") {
			t.Errorf("expected error to mention status 429, got %q", err.Error())
		}
	})

	t.Run("returns error for malformed JSON", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `not json`)
		}))
		defer server.Close()

		// Act
		var response YahooResponse
		_, err := response.GetResponse(server.URL)

		// Assert
		if err == nil {
			t.Fatal("expected decode error, got nil")
		}
	})

	t.Run("rejects nil HTTP client", func(t *testing.T) {
		// Act
		var response YahooResponse
		_, err := response.GetResponseWithClient(nil, "http://localhost/")

		// Assert
		if err == nil {
			t.Fatal("expected error for nil client, got nil")
		}
	})
}
