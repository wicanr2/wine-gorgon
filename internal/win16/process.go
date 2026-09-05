package win16

import (
	"fmt"

	"github.com/wicanr2/wine-gorgon/internal/cpu"
	"github.com/wicanr2/wine-gorgon/internal/ne"
)

// Process 是「一個載好的模組 ＋ 一顆在上面跑的 CPU」。
//
// API 攔截在這裡收口：CPU 只知道「有人 far call 到某個 selector」，
// 由 Process 把它翻成「這是 GDI 的第 45 號」，再交給登記的處理器。
type Process struct {
	Mod *Module
	CPU *cpu.CPU

	// Handlers 的鍵是 ne.Import.Key()（`GDI.#45`、`KERNEL.GLOBALALLOC`）。
	Handlers map[string]Handler

	// Trace 記下每一次 API 呼叫，順序即發生順序。
	Trace []Call

	// TraceLimit 是 Trace 的上限，避免長跑把記憶體吃光；0 表示不限。
	TraceLimit int
}

// Handler 實作一支 API。它拿到的是「呼叫端的回傳位址已經在堆疊上」的狀態，
// 和真正的被呼叫方一樣；做完事之後要自己呼叫 p.CPU.RetFar(參數位元組數)。
type Handler func(p *Process, imp ne.Import) error

// Call 是一次 API 呼叫的紀錄。
type Call struct {
	Import ne.Import
	Steps  uint64
	FromCS uint16
	FromIP uint16
}

// UnhandledAPIError 是「攔到了，但沒有人實作」。
//
// 這是這個專案最常見的錯誤，所以它帶齊了往下走需要的全部資訊：
// 哪一支 API、誰呼叫的、跑到第幾步。
type UnhandledAPIError struct {
	Import ne.Import
	Call   Call
}

func (e *UnhandledAPIError) Error() string {
	return fmt.Sprintf("win16: 未實作的 API %s（由 %04X:%04X 呼叫，第 %d 步）",
		e.Import.Key(), e.Call.FromCS, e.Call.FromIP, e.Call.Steps)
}

// NewProcess 建立行程並把暫存器設成 NE 進入點的初始狀態。
//
// 初始狀態的來源全部是檔頭：CS:IP 與 SS:SP 直接取 `ne_csip`／`ne_sssp`，
// DS 取自動資料段。`ne_sssp` 的兩種特例（段號 0、SP 0）照 Windows 載入器
// 的規則補：段號 0 表示堆疊就在 DGROUP 裡，SP 0 表示指到那塊的尾巴。
func NewProcess(mod *Module) (*Process, error) {
	p := &Process{Mod: mod, Handlers: map[string]Handler{}, TraceLimit: 100000}
	c := cpu.New(mod.Mem)
	p.CPU = c

	seg, off, err := mod.Image.Entry()
	if err != nil {
		return nil, err
	}
	c.Seg[cpu.CS], c.IP = SegSelector(seg), off

	dsSel := uint16(0)
	if mod.Image.AutoData != 0 {
		dsSel = SegSelector(mod.Image.AutoData)
	}
	c.Seg[cpu.DS] = dsSel
	c.Seg[cpu.ES] = dsSel

	ssSeg := int(mod.Image.SSSP >> 16)
	sp := uint16(mod.Image.SSSP)
	ssSel := dsSel
	if ssSeg != 0 {
		ssSel = SegSelector(ssSeg)
	}
	if ssSel == 0 {
		return nil, fmt.Errorf("win16: 這個模組既沒有自動資料段也沒有堆疊段，無法設定 SS")
	}
	if sp == 0 {
		blk, ok := mod.Mem.Block(ssSel)
		if !ok {
			return nil, fmt.Errorf("win16: 堆疊 selector %04X 沒有配置", ssSel)
		}
		sp = uint16(len(blk.Data)) // 長度剛好 0x10000 時會變成 0，那也正是硬體的行為
	}
	c.Seg[cpu.SS], c.R[cpu.SP] = ssSel, sp

	c.OnFarCall = p.onFarCall
	return p, nil
}

// onFarCall 是 CPU 每次 far 轉移的回呼；只有落到 thunk 段的才算 API。
func (p *Process) onFarCall(c *cpu.CPU, sel, off uint16) (bool, error) {
	if sel != ThunkSel {
		return false, nil
	}
	imp, ok := p.Mod.ImportAt(off)
	if !ok {
		return false, fmt.Errorf("win16: 跳到 thunk 段的 %04X，但那裡沒有匯入項", off)
	}

	// 呼叫端在堆疊上：far call 剛推入 CS:IP，所以 [SP] 是回傳位移、
	// [SP+2] 是回傳段。拿它當「誰呼叫的」比記 CPU 現值準確。
	fromIP, _ := p.Mod.Mem.ReadU16(c.Seg[cpu.SS], c.R[cpu.SP])
	fromCS, _ := p.Mod.Mem.ReadU16(c.Seg[cpu.SS], c.R[cpu.SP]+2)
	call := Call{Import: imp, Steps: c.Steps, FromCS: fromCS, FromIP: fromIP}
	if p.TraceLimit == 0 || len(p.Trace) < p.TraceLimit {
		p.Trace = append(p.Trace, call)
	}

	h, ok := p.Handlers[imp.Key()]
	if !ok {
		return true, &UnhandledAPIError{Import: imp, Call: call}
	}
	return true, h(p, imp)
}

// Run 跑到 Halt、錯誤或步數上限。
func (p *Process) Run(maxSteps uint64) error { return p.CPU.Run(maxSteps) }
