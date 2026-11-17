package datasource

import "fmt"

// YahooFetch 逐个拉取 symbol 的历史数据。
// 错误一律上抛给调用方(重试/跳过/终止由编排层决定)——库代码不得
// log.Fatal 杀死宿主进程。TODO:返回类型改成 map;单 symbol 失败不
// 阻断整体的策略随导入编排移入 pipeline 包。
func YahooFetch(symbols []string, start, end, interval string) ([]YahooResponse, error) {
	var responseList []YahooResponse
	for _, symbol := range symbols {
		var response YahooResponse
		url, err := YahooApiMaker(symbol, start, end, "true", interval)
		if err != nil {
			return nil, fmt.Errorf("build Yahoo request for %q: %w", symbol, err)
		}
		response, err = response.GetResponse(url)
		if err != nil {
			return nil, fmt.Errorf("fetch Yahoo data for %q: %w", symbol, err)
		}
		responseList = append(responseList, response)
	}
	return responseList, nil
}
