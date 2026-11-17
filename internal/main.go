package main

import (
	"FinanceSimulator/datasource"
	"fmt"
	"log"
)

func main() {
	SymbolList := []string{"AAPL"}
	responseList, err := datasource.YahooFetch(SymbolList, "2007-01-01", "2026-11-04", "1d")
	if err != nil {
		log.Fatalln(err)
	}
	AAPLAction, err := responseList[0].CorporateActions()
	if err != nil {
		log.Fatalln(err)
	}
	for _, action := range AAPLAction {
		if action.Type != datasource.ActionSplit {
			continue
		}
		fmt.Println(action)
	}
}
