package storage

import (
	"FinanceSimulator/datasource"
	"io"
	"os"

	"github.com/parquet-go/parquet-go"
)

// Parquet读写

type Record struct {
	Bars   []datasource.Bar     `parquet:"bars, list"`
	Tf     datasource.Timeframe `parquet:"timeframe, zstd"`
	Symbol string               `parquet:"symbol, zstd"`
}

// ParquetWrite 将相同索引的 bars、timeframe 和 symbol 组成一条 record 写入 data.parquet。
func ParquetWrite(bars [][]datasource.Bar, tf []datasource.Timeframe, symbol []string) error {
	var recordList []Record
	for i := range bars {
		recordList = append(recordList, Record{
			Bars:   bars[i],
			Tf:     tf[i],
			Symbol: symbol[i],
		})
	}
	f, err := os.OpenFile("data.parquet",
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if err != nil {
		return err
	}
	writer := parquet.NewWriter(f)
	for _, row := range recordList {
		err = writer.Write(row)
		if err != nil {
			return err
		}
	}
	_ = writer.Close()
	_ = f.Close()
	return nil
}

// ParquetRead 从 filename 读取所有 record，并按写入时的 record 边界返回 bars。
func ParquetRead(filename string) ([][]datasource.Bar, error) {
	rf, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer rf.Close()

	fileInfo, err := rf.Stat()
	if err != nil {
		return nil, err
	}
	parquetFile, err := parquet.OpenFile(rf, fileInfo.Size())
	if err != nil {
		return nil, err
	}

	pf := parquet.NewReader(parquetFile)
	recordList := make([]Record, 0)
	for {
		var record Record
		err = pf.Read(&record)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		recordList = append(recordList, record)
	}
	result := make([][]datasource.Bar, 0, len(recordList))
	for _, record := range recordList {
		result = append(result, record.Bars)
	}
	return result, nil
}
