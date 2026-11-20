package storage

import (
	"FinanceSimulator/datasource"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

func TestParquetWriteAppendsCompleteRecords(t *testing.T) {
	t.Chdir(t.TempDir())
	want := []Record{
		{Bars: []datasource.Bar{{Time: time.Date(2024, 1, 2, 14, 30, 0, 0, time.UTC), Close: 185}}, Tf: datasource.TF1d, Symbol: "AAPL"},
		{Bars: []datasource.Bar{{Time: time.Date(2024, 1, 3, 14, 30, 0, 0, time.UTC), Close: 375}}, Tf: datasource.TF5m, Symbol: "MSFT"},
	}
	for _, record := range want {
		if err := ParquetWrite([][]datasource.Bar{record.Bars}, []datasource.Timeframe{record.Tf}, []string{record.Symbol}); err != nil {
			t.Fatalf("ParquetWrite %s: %v", record.Symbol, err)
		}
	}
	got, err := parquet.ReadFile[Record]("data.parquet")
	if err != nil {
		t.Fatalf("read appended Parquet file: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appended records = %#v, want %#v", got, want)
	}
}

func TestParquetWriteSerializesConcurrentBatches(t *testing.T) {
	t.Chdir(t.TempDir())
	const writers = 8
	start := make(chan struct{})
	results := make(chan error, writers)
	for index := range writers {
		go func() {
			<-start
			bars := [][]datasource.Bar{{{Time: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: float64(index)}}}
			results <- ParquetWrite(bars, []datasource.Timeframe{datasource.TF1d}, []string{fmt.Sprint(index)})
		}()
	}
	close(start)
	for range writers {
		if err := <-results; err != nil {
			t.Errorf("concurrent ParquetWrite: %v", err)
		}
	}
	got, err := parquet.ReadFile[Record]("data.parquet")
	if err != nil {
		t.Fatalf("read concurrent writes: %v", err)
	}
	if len(got) != writers {
		t.Errorf("concurrent writes produced %d records, want %d", len(got), writers)
	}
	symbols := make(map[string]bool)
	for _, record := range got {
		symbols[record.Symbol] = true
	}
	for index := range writers {
		if !symbols[fmt.Sprint(index)] {
			t.Errorf("concurrent writes lost record %d", index)
		}
	}
}

func TestParquetWriteRejectsMismatchedBatches(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := ParquetWrite([][]datasource.Bar{{}}, nil, []string{"AAPL"}); err == nil {
		t.Error("ParquetWrite accepted a missing timeframe")
	}
	if err := ParquetWrite(nil, nil, []string{"AAPL"}); err == nil {
		t.Error("ParquetWrite accepted an extra symbol")
	}
	if _, err := os.Stat("data.parquet"); !os.IsNotExist(err) {
		t.Errorf("stat after invalid batches = %v, want missing file", err)
	}
}

func TestParquetWriteEmptyBatchIsNoOp(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := ParquetWrite(nil, nil, nil); err != nil {
		t.Fatalf("ParquetWrite empty batch: %v", err)
	}
	if _, err := os.Stat("data.parquet"); !os.IsNotExist(err) {
		t.Errorf("stat after empty batch = %v, want missing file", err)
	}
}

func TestParquetWritePreservesCorruptFile(t *testing.T) {
	t.Chdir(t.TempDir())
	const path = "data.parquet"
	original := []byte("not a Parquet file")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("create corrupt Parquet fixture: %v", err)
	}
	if err := ParquetWrite([][]datasource.Bar{{}}, []datasource.Timeframe{datasource.TF1d}, []string{"AAPL"}); err == nil {
		t.Error("ParquetWrite accepted a corrupt file")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original file: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Error("ParquetWrite modified the corrupt file")
	}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read output directory: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("output directory has %d entries, want original file only", len(files))
	}
}

func TestParquetWritePreservesPreviouslyConcatenatedFile(t *testing.T) {
	t.Chdir(t.TempDir())
	const path = "data.parquet"
	var previous bytes.Buffer
	for _, symbol := range []string{"AAPL", "MSFT"} {
		writer := parquet.NewWriter(&previous)
		if err := writer.Write(Record{Tf: datasource.TF1d, Symbol: symbol}); err != nil {
			t.Fatalf("write standalone Parquet segment: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close standalone Parquet segment: %v", err)
		}
	}
	if err := os.WriteFile(path, previous.Bytes(), 0o600); err != nil {
		t.Fatalf("write concatenated fixture: %v", err)
	}
	if err := ParquetWrite([][]datasource.Bar{{}}, []datasource.Timeframe{datasource.TF5m}, []string{"TSLA"}); err == nil {
		t.Error("ParquetWrite accepted a file containing independent Parquet segments")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read concatenated fixture: %v", err)
	}
	if !bytes.Equal(got, previous.Bytes()) {
		t.Error("ParquetWrite modified the concatenated file")
	}
}

func TestParquetWriteAppendsToPreAdjustmentSchema(t *testing.T) {
	t.Chdir(t.TempDir())
	type oldBar struct {
		Time     time.Time
		Open     float64
		High     float64
		Low      float64
		Close    float64
		Volume   int64
		AdjClose float64
	}
	type oldRecord struct {
		Bars   []oldBar             `parquet:"bars, list"`
		Tf     datasource.Timeframe `parquet:"timeframe, zstd"`
		Symbol string               `parquet:"symbol, zstd"`
	}
	stamp := time.Date(2024, 1, 2, 14, 30, 0, 0, time.UTC)
	if err := parquet.WriteFile("data.parquet", []oldRecord{{
		Bars: []oldBar{{Time: stamp, Close: 185}}, Tf: datasource.TF1d, Symbol: "AAPL",
	}}); err != nil {
		t.Fatalf("write old Parquet schema: %v", err)
	}
	wantOriginal := []datasource.Bar{{Time: stamp, Close: 185, PriceBasis: datasource.PriceBasisUnknown}}
	if got, err := ParquetRead("data.parquet"); err != nil || !reflect.DeepEqual(got, [][]datasource.Bar{wantOriginal}) {
		t.Fatalf("read old Parquet schema = %#v, %v, want %#v", got, err, wantOriginal)
	}
	newBars := []datasource.Bar{{Time: stamp, Close: 375, PriceBasis: datasource.PriceSplitAdjusted}}
	if err := ParquetWrite([][]datasource.Bar{newBars}, []datasource.Timeframe{datasource.TF5m}, []string{"MSFT"}); err != nil {
		t.Fatalf("append to old Parquet schema: %v", err)
	}
	want := []Record{
		{Bars: wantOriginal, Tf: datasource.TF1d, Symbol: "AAPL"},
		{Bars: newBars, Tf: datasource.TF5m, Symbol: "MSFT"},
	}
	if got, err := parquet.ReadFile[Record]("data.parquet"); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("appended records = %#v, %v, want %#v", got, err, want)
	}
}

// TestParquetWrite 验证 ParquetWrite 使用传入的行情创建包含预期 record 数量的文件。
func TestParquetWrite(t *testing.T) {
	// ParquetWrite 当前固定写入当前目录的 data.parquet，因此将整个测试隔离到临时目录。
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	temporaryDirectory := t.TempDir()
	if err := os.Chdir(temporaryDirectory); err != nil {
		t.Fatalf("Chdir temporary directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(workingDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	barsList := [][]datasource.Bar{
		{{Time: time.Date(2024, 1, 2, 14, 30, 0, 0, time.UTC), Open: 187.15, High: 188.44, Low: 183.89, Close: 185.64, Volume: 82_488_700}},
		{{Time: time.Date(2024, 1, 3, 14, 30, 0, 0, time.UTC), Open: 184.22, High: 185.88, Low: 183.43, Close: 184.25, Volume: 58_414_400}},
	}
	timeframes := []datasource.Timeframe{datasource.TF1d, datasource.TF1d}
	symbols := []string{"AAPL", "MSFT"}

	if err := ParquetWrite(barsList, timeframes, symbols); err != nil {
		t.Fatalf("ParquetWrite: %v", err)
	}

	path := filepath.Join(temporaryDirectory, "data.parquet")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open Parquet file: %v", err)
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil {
		t.Fatalf("stat Parquet file: %v", err)
	}
	parquetFile, err := parquet.OpenFile(file, fileInfo.Size())
	if err != nil {
		t.Fatalf("open Parquet metadata: %v", err)
	}
	if got := parquetFile.NumRows(); got != 2 {
		t.Errorf("Parquet record count = %d, want 2", got)
	}
}

// TestParquetRead 验证 ParquetRead 能读取有效文件，并将无效文件的读取错误返回给调用方。
func TestParquetRead(t *testing.T) {
	// ParquetWrite 当前固定写入当前目录的 data.parquet，因此将整个测试隔离到临时目录。
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	temporaryDirectory := t.TempDir()
	if err := os.Chdir(temporaryDirectory); err != nil {
		t.Fatalf("Chdir temporary directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(workingDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	barsList := [][]datasource.Bar{
		{{Time: time.Date(2024, 1, 2, 14, 30, 0, 0, time.UTC), Open: 187.15, High: 188.44, Low: 183.89, Close: 185.64, Volume: 82_488_700}},
	}
	if err := ParquetWrite(barsList, []datasource.Timeframe{datasource.TF1d}, []string{"AAPL"}); err != nil {
		t.Fatalf("ParquetWrite fixture: %v", err)
	}

	validFile := filepath.Join(temporaryDirectory, "data.parquet")
	readBars, err := ParquetRead(validFile)
	if err != nil {
		t.Fatalf("ParquetRead valid file: %v", err)
	}
	if !reflect.DeepEqual(readBars, barsList) {
		t.Errorf("ParquetRead bars = %#v, want %#v", readBars, barsList)
	}

	invalidFile := filepath.Join(temporaryDirectory, "invalid.parquet")
	if err := os.WriteFile(invalidFile, []byte("not a Parquet file"), 0o600); err != nil {
		t.Fatalf("write invalid fixture: %v", err)
	}

	var readBarsFromInvalidFile [][]datasource.Bar
	var readErr error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("ParquetRead invalid file panicked: %v", recovered)
			}
		}()
		readBarsFromInvalidFile, readErr = ParquetRead(invalidFile)
	}()
	if readErr == nil {
		t.Error("ParquetRead invalid file error = nil, want a read error")
	}
	if readBarsFromInvalidFile != nil {
		t.Errorf("ParquetRead invalid file bars = %#v, want nil", readBarsFromInvalidFile)
	}
}
