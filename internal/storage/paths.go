package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"FinanceSimulator/datasource"
)

// symbolPattern 美股 symbol 白名单:字母、数字、点、短横线,1-12 字符。
// 覆盖 BRK.B / BF-B 等常见形态;平台只做股票与 ETF,指数(^GSPC)、
// 外汇(EURUSD=X)等带特殊字符的代码按设计不予入库。
var symbolPattern = regexp.MustCompile(`^[A-Za-z0-9.\-]{1,12}$`)

// validateSymbol 在 symbol 参与文件路径构造前做白名单校验。
// symbol 未来将来自 HTTP API 的用户输入(设计 4.1 的 per-symbol 存储布局),
// 本函数是防路径穿越的第一道防线。
func validateSymbol(symbol string) error {
	if !symbolPattern.MatchString(symbol) {
		return fmt.Errorf("invalid symbol %q", symbol)
	}
	return nil
}

// barsPath 是设计 4.1 行情文件布局(data/bars/{tf}/{SYMBOL}/year={YYYY}.parquet)
// 的强制构造函数:任何代码要落地这个布局都必须经此函数,不得自行拼路径。
//
// 双重防线:白名单校验挡恶意 symbol;即使校验被绕过,filepath.Clean 后的
// containment 检查也会拒绝越出数据目录的路径。
func barsPath(dataDir string, tf datasource.Timeframe, symbol string, year int) (string, error) {
	if err := validateSymbol(symbol); err != nil {
		return "", err
	}
	path := filepath.Join(dataDir, "bars", string(tf), symbol, fmt.Sprintf("year=%d.parquet", year))
	cleanRoot := filepath.Clean(dataDir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(path)+string(os.PathSeparator), cleanRoot) {
		return "", fmt.Errorf("path escapes data directory: %q", path)
	}
	return path, nil
}
