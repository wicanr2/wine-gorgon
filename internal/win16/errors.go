package win16

import "fmt"

// UnsupportedError 是「這件事這一層還沒做」，和「做錯了」要分得開：
// 前者是工作清單上的一項，後者是 bug。
type UnsupportedError struct{ Msg string }

func (e *UnsupportedError) Error() string { return "win16: 尚未支援：" + e.Msg }

func errUnsupported(format string, a ...any) error {
	return &UnsupportedError{Msg: fmt.Sprintf(format, a...)}
}
