package datasource

import "testing"

// 纵深防御 1:库代码不得 log.Fatal 杀死宿主进程,错误必须上抛给调用方。
// 用非法日期触发 YahooApiMaker 校验失败(无需真实 HTTP 请求)。
func TestYahooFetch_ReturnsErrorInsteadOfExit(t *testing.T) {
	if _, err := YahooFetch([]string{"AAPL"}, "not-a-date", "2024-02-01", "1d"); err == nil {
		t.Fatal("want error for invalid dates, not process exit")
	}
}
