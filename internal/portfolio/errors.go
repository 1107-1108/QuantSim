package portfolio

import "errors"

// 哨兵错误(Sentinel Errors):包内所有可预期的失败都归约到这几个值。
// 调用方判断错误类型时一律用 errors.Is(err, ErrXxx),不要比较错误字符串——
// 因为实际返回的错误都用 fmt.Errorf("%w: 上下文", ErrXxx) 包装过,直接相等比较会失败。
var (
	// ErrOversell 卖出数量超过持仓(超卖)。事件循环应先做持仓校验,
	// 所以出现这个错说明上游校验漏了,属于 bug。
	ErrOversell = errors.New("sell quantity exceeds position")

	// ErrNegativeCash 买入成交后现金为负。同样属于上游购买力校验漏了的 bug。
	ErrNegativeCash = errors.New("negative cash after fill")

	// ErrInvalidFill 成交记录本身非法:空 symbol、非法方向、数量/价格非正、
	// NaN、负佣金。matcher 产出的 Fill 本应合法,出现即上游 bug。
	ErrInvalidFill = errors.New("invalid fill")

	// ErrUnknownCorporateAction 未建模的公司行为类型(如配股、送转)。
	// 宁可报错也不静默跳过——静默跳过会让账本悄悄失真。
	ErrUnknownCorporateAction = errors.New("unknown corporate action type")

	// ErrMissingMark 估值时持仓标的缺少市价。不静默用成本价兜底,
	// 否则净值曲线会把数据问题掩盖掉。
	ErrMissingMark = errors.New("missing mark for held symbol")
)
