package matcher

import "errors"

// 哨兵错误:均可 errors.Is 判断,与 portfolio 包同款模式。
var (
	ErrInvalidOrder = errors.New("invalid order")
	ErrInvalidBar   = errors.New("invalid bar")
)
