package storage

import (
	"FinanceSimulator/datasource"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

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
