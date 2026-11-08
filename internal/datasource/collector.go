package datasource

import (
	"log"
)

// TODO 返回类型改成map
func YahooFetch(symbols []string, start, end, interval string) []YahooResponse {
	var responseList []YahooResponse
	for _, symbol := range symbols {
		var response YahooResponse
		url, err := YahooApiMaker(symbol, start, end, "true", interval)
		if err != nil {
			log.Fatalln(err)
		}
		response, err = response.GetResponse(url)
		responseList = append(responseList, response)
	}
	return responseList
}
