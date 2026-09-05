package win16

import (
	"github.com/wicanr2/wine-gorgon/internal/cpu"
	"github.com/wicanr2/wine-gorgon/internal/ne"
)

// WIN87EM 是 Borland 的 8087 模擬器。它只匯出一個入口 `__FPMATH`，
// 用 **BX 選功能**、不吃堆疊參數——所以走 RawHandler。
//
// CIV.EXE 的啟動碼在 `000F:41A7` 附近連呼叫三次：
//
//	BB 02 00          mov  bx, 2
//	9A 18 00 0F F0    call far __FPMATH
//	...
//	C7 06 10 51 18 00 mov  [5110], 18h     ; 把 __FPMATH 的 far 指標
//	C7 06 12 51 0F F0 mov  [5112], F00Fh   ; 存進全域變數
//	33 DB             xor  bx, bx
//	9A ...            call far __FPMATH
//
// 也就是「初始化 ＋ 記下入口位址」。**目前一律回成功而不做任何浮點運算**：
// 還沒量到 CIV.EXE 哪裡真的做浮點。若之後看到數值對不上，這裡是第一個
// 要回頭查的地方。

// RegisterWin87EM 登記浮點模擬器入口。
func RegisterWin87EM(p *Process) {
	p.RawHandlers["WIN87EM.#1"] = func(p *Process, _ ne.Import) error {
		c := p.CPU
		p.FPMathCodes = append(p.FPMathCodes, c.R16(cpu.BX))
		c.SetR16(cpu.AX, 0)
		return c.RetFar(0)
	}
}
