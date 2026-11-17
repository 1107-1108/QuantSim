package storage

import (
	"path/filepath"
	"testing"

	"FinanceSimulator/datasource"
)

// R1:真实形态的美股 symbol 必须放行(字母数字点短横线,1-12 字符)
func TestValidateSymbol_AcceptsRealisticSymbols(t *testing.T) {
	for _, symbol := range []string{"AAPL", "BRK.B", "BF-B", "X", "VOD9"} {
		if err := validateSymbol(symbol); err != nil {
			t.Errorf("symbol %q: unexpected error %v", symbol, err)
		}
	}
}

// R1:路径穿越、分隔符、控制字符、超长、非 ASCII 全部拒绝
func TestValidateSymbol_RejectsTraversalAndMalformed(t *testing.T) {
	cases := []string{
		"../etc/passwd", `..\windows`, "a/b", `a\b`, "",
		"TOOLONGSYMBOL13", "AP PL", "AAPL\x00", "^GSPC", "EURUSD=X", "苹果",
	}
	for _, symbol := range cases {
		if err := validateSymbol(symbol); err == nil {
			t.Errorf("symbol %q: want rejection", symbol)
		}
	}
}

// R1:正常构造 = 设计 4.1 布局,且路径必须被限制在数据目录内
func TestBarsPath_BuildsContainedPath(t *testing.T) {
	got, err := barsPath("data", datasource.TF1d, "AAPL", 2024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join("data", "bars", "1d", "AAPL", "year=2024.parquet")
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

// R1:containment 是白名单之外的第二道防线——即使恶意输入绕过校验,越界路径也被拒绝
func TestBarsPath_RejectsTraversalSymbol(t *testing.T) {
	if _, err := barsPath("data", datasource.TF1d, "../../etc", 2024); err == nil {
		t.Fatal("want rejection for traversal symbol")
	}
}

func TestBarsPath_RejectsEmptyDataDir(t *testing.T) {
	if _, err := barsPath("", datasource.TF1d, "AAPL", 2024); err == nil {
		t.Fatal("want rejection for empty data dir (no controlled root)")
	}
}
