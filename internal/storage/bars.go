package storage

import (
	"FinanceSimulator/datasource"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/parquet-go/parquet-go"
)

const barsFile = "data.parquet"

var parquetWriteMu sync.Mutex

type Record struct {
	Bars   []datasource.Bar     `parquet:"bars, list"`
	Tf     datasource.Timeframe `parquet:"timeframe, zstd"`
	Symbol string               `parquet:"symbol, zstd"`
}

// ParquetWrite 将相同索引的 bars、timeframe 和 symbol 组成 record，追加到 data.parquet。
func ParquetWrite(bars [][]datasource.Bar, tf []datasource.Timeframe, symbol []string) error {
	if len(bars) != len(tf) || len(bars) != len(symbol) {
		return fmt.Errorf("Parquet batch lengths differ: bars=%d, timeframes=%d, symbols=%d", len(bars), len(tf), len(symbol))
	}
	if len(bars) == 0 {
		return nil
	}

	parquetWriteMu.Lock()
	defer parquetWriteMu.Unlock()

	records, err := readParquetRecords(barsFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read existing %s: %w", barsFile, err)
	}
	for i := range bars {
		records = append(records, Record{
			Bars:   bars[i],
			Tf:     tf[i],
			Symbol: symbol[i],
		})
	}

	file, err := os.CreateTemp(".", ".data.parquet-*")
	if err != nil {
		return fmt.Errorf("create temporary Parquet file: %w", err)
	}
	defer os.Remove(file.Name())

	writer := parquet.NewGenericWriter[Record](file)
	_, writeErr := writer.Write(records)
	if err := errors.Join(writeErr, writer.Close(), file.Close()); err != nil {
		return fmt.Errorf("write temporary Parquet file: %w", err)
	}
	return os.Rename(file.Name(), barsFile)
}

// ParquetRead 从 filename 读取所有 record，并按写入时的 record 边界返回 bars。
func ParquetRead(filename string) ([][]datasource.Bar, error) {
	records, err := readParquetRecords(filename)
	if err != nil {
		return nil, err
	}
	result := make([][]datasource.Bar, 0, len(records))
	for _, record := range records {
		result = append(result, record.Bars)
	}
	return result, nil
}

func readParquetRecords(filename string) (records []Record, err error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	parquetFile, err := openParquetFile(file, fileInfo.Size())
	if err != nil {
		return nil, err
	}
	_, hasPriceBasis := parquetFile.Schema().Lookup("bars", "list", "element", "PriceBasis")

	reader := parquet.NewReader(parquetFile)
	defer func() { err = errors.Join(err, reader.Close()) }()
	for {
		var record Record
		readErr := reader.Read(&record)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
		if !hasPriceBasis {
			for index := range record.Bars {
				record.Bars[index].PriceBasis = datasource.PriceBasisUnknown
			}
		}
		records = append(records, record)
	}
	if int64(len(records)) != parquetFile.NumRows() {
		return nil, fmt.Errorf("read %d Parquet rows, expected %d", len(records), parquetFile.NumRows())
	}
	return records, nil
}

func openParquetFile(file *os.File, size int64) (*parquet.File, error) {
	const headerSize = 4
	var trailer [8]byte
	if size < headerSize+int64(len(trailer)) {
		return nil, fmt.Errorf("Parquet file is too small: %d bytes", size)
	}
	trailerOffset := size - int64(len(trailer))
	if _, err := file.ReadAt(trailer[:], trailerOffset); err != nil {
		return nil, err
	}
	footerOffset := trailerOffset - int64(binary.LittleEndian.Uint32(trailer[:4]))
	if footerOffset < headerSize {
		return nil, fmt.Errorf("invalid Parquet footer offset: %d", footerOffset)
	}
	parquetFile, err := parquet.OpenFile(file, size)
	if err != nil {
		return nil, err
	}

	metadata := parquetFile.Metadata()
	if strings.HasPrefix(metadata.CreatedBy, "github.com/parquet-go/parquet-go ") {
		// parquet-go writes the footer immediately after the page indexes. Files
		// concatenated by older writers can otherwise read from stale offsets.
		indexesEnd := int64(headerSize)
		for _, group := range metadata.RowGroups {
			for _, column := range group.Columns {
				indexesEnd = max(indexesEnd, column.OffsetIndexOffset+int64(column.OffsetIndexLength))
			}
		}
		// A shifted footer suggests concatenation only if the predicted join
		// contains one file's closing magic and the next file's opening magic.
		firstFileSize := footerOffset - indexesEnd
		if indexesEnd > headerSize && firstFileSize >= headerSize+int64(len(trailer)) {
			var magic [8]byte
			if _, err := file.ReadAt(magic[:], firstFileSize-headerSize); err != nil {
				return nil, err
			}
			if string(magic[:]) == "PAR1PAR1" {
				return nil, fmt.Errorf("concatenated Parquet files at byte %d", firstFileSize)
			}
		}
	}
	return parquetFile, nil
}
