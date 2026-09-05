package win16

import (
	"fmt"

	"github.com/wicanr2/wine-gorgon/internal/cpu"
)

// RetTrapSel 是「回到 Go 這一側」的假 selector。
//
// 從 Go 呼叫 16 位元程式碼（視窗程序、對話框程序、列舉回呼）時，把這個
// selector 當成返回位址推進去；CPU 一跳到它就停。它**不是**一塊記憶體，
// 永遠不會被執行——所以不必配置，也不會被誤讀成程式碼。
const RetTrapSel = 0xF00E

// callDepthLimit 擋住互相呼叫收不了尾的情形。
const callDepthLimit = 32

// Call16 從 Go 這一側呼叫 16 位元程式碼，參數照 pascal 慣例由左往右推。
//
// 回傳值是 DX:AX。呼叫前後的暫存器狀態會還原，只有 AX／DX 是輸出——
// 這一點和真 Windows 不同（真的是直接跳進去），但對「Go 呼叫一支回呼」
// 這個用法來說，不動到呼叫端的執行狀態才是對的。
func (p *Process) Call16(sel, off uint16, args ...uint16) (uint32, error) {
	if p.callDepth >= callDepthLimit {
		return 0, fmt.Errorf("win16: 回呼巢狀超過 %d 層（%04X:%04X）", callDepthLimit, sel, off)
	}
	c := p.CPU
	saved := struct {
		R     [8]uint32
		Seg   [4]uint16
		IP    uint16
		Flags uint16
	}{c.R, c.Seg, c.IP, c.Flags}

	for _, a := range args {
		if err := c.PushWord(a); err != nil {
			return 0, err
		}
	}
	if err := c.PushWord(RetTrapSel); err != nil {
		return 0, err
	}
	if err := c.PushWord(0); err != nil {
		return 0, err
	}
	c.Seg[cpu.CS], c.IP = sel, off

	p.callDepth++
	defer func() { p.callDepth-- }()

	start := c.Steps
	for c.Seg[cpu.CS] != RetTrapSel {
		if c.Steps-start > p.CallStepLimit {
			return 0, fmt.Errorf("win16: 回呼 %04X:%04X 跑了 %d 條指令還沒回來",
				sel, off, c.Steps-start)
		}
		if err := c.Step(); err != nil {
			return 0, err
		}
	}
	ret := uint32(c.R16(cpu.DX))<<16 | uint32(c.R16(cpu.AX))

	c.R, c.Seg, c.IP, c.Flags = saved.R, saved.Seg, saved.IP, saved.Flags
	return ret, nil
}
