package win16

import (
	"fmt"
	"time"

	"github.com/wicanr2/wine-gorgon/internal/cpu"
	"github.com/wicanr2/wine-gorgon/internal/ne"
	"github.com/wicanr2/wine-gorgon/internal/winapi"
)

// Process 是「一個載好的模組 ＋ 一顆在上面跑的 CPU」。
//
// API 攔截在這裡收口：CPU 只知道「有人 far call 到某個 selector」，
// 由 Process 把它翻成「這是 GDI 的第 45 號」，再交給登記的處理器。
type Process struct {
	Mod *Module
	CPU *cpu.CPU

	// Handlers 的鍵是 ne.Import.Key()（`GDI.#45`）。
	Handlers map[string]Handler

	// RawHandlers 給不吃 pascal 慣例的入口用（`WIN87EM.__FPMATH` 靠 BX
	// 選功能）。它要自己彈堆疊，所以也自己負責 RetFar。
	RawHandlers map[string]RawHandler

	// Trace 記下前 TraceLimit 次 API 呼叫，順序即發生順序。
	Trace []Call

	// Recent 是最後 64 次呼叫（Trace 滿了之後才開始收）。
	Recent []Call

	// LastCall 是最近一次 API 呼叫，處理器可以用它報「誰呼叫我的」。
	LastCall Call

	// Calls 是總次數，callCount 是逐支的次數；兩者都沒有上限。
	Calls     int
	callCount map[string]int

	// TraceLimit 是 Trace 的上限，避免長跑把記憶體吃光；0 表示不限。
	TraceLimit int

	// PSP 是那塊放命令列的 selector（`InitTask` 回傳的 ES）。
	PSP uint16

	// Env 是 DOS 環境區塊的 selector（`GetDOSEnvironment` 回傳的段）。
	Env uint16

	// ModulePath 是 `GetModuleFileName` 回報的路徑。它會出現在遊戲用來
	// 找資料檔的字串裡，所以要和實際的資料目錄對得起來。
	ModulePath string

	// MessageBoxes 記下遊戲彈過的訊息框。沒有畫面的時候，這是它唯一
	// 會主動說話的管道。
	MessageBoxes []MessageBoxCall

	// Notes 是「做了但不完整」的清單，去重。它是下一輪的工作清單。
	Notes     []string
	seenNotes map[string]bool

	// CurrentDrive／CurrentDir 是行程看到的 DOS 目前位置（0=A、2=C）。
	CurrentDrive uint8
	CurrentDir   string

	// ScreenW／ScreenH 是回報給 GetSystemMetrics 的螢幕尺寸。
	// 原版 Civilization 跑在 640×480；改這個會改變版面計算。
	ScreenW, ScreenH int

	// Screen 是整個桌面的畫布。視窗 DC 就是「Screen ＋ 一個原點與裁切」。
	Screen *Surface

	// SysPalette 是實體調色盤（256 格）。逐點比對比的是索引，
	// 這張表只在輸出 PNG 時用到。
	SysPalette [256]RGB

	// Metrics 是 GetSystemMetrics 的表。預設值見 defaultMetrics，
	// **是假說**，M4 之前要逐項核對。
	Metrics map[int]int

	// GDI／USER 的物件表與視窗表。
	Objects     *Objects
	Classes     map[string]*Class
	Windows     map[uint16]*Window
	WindowOrder []uint16 // 建立順序；重畫依這個順序掃
	nextHWnd    uint16

	// Queue 是訊息佇列，Timers 是計時器。
	Queue  []Msg
	Timers []Timer

	// Desktop 是桌面視窗的 handle。
	Desktop uint16

	// 滑鼠與焦點狀態。
	Capture, Focus   uint16
	CursorX, CursorY int
	nextTimerID      uint16
	cursorHandle     uint16
	iconHandle       uint16

	// PalMap 是「邏輯調色盤索引 → 實體調色盤索引」的對應，
	// 由 RealizePalette 建立。
	PalMap []byte

	// TextOuts／FontFiles／Blits 是量測欄位：畫不出來的字、載過的字型檔、
	// blit 次數。它們是下一輪的工作清單。
	TextOuts   []TextOutCall
	FontFiles  []string
	Blits      int
	BlitsBadDC int
	stock      map[uint16]uint16

	// Quit 是收到 PostQuitMessage 之後的狀態。
	Quit     bool
	QuitCode uint16

	// CallStepLimit 是一次回呼最多能跑幾條指令。回呼不回來多半是我們
	// 給錯了參數，讓它無聲地跑下去只會把問題推遠。
	CallStepLimit uint64
	callDepth     int

	// FS 是這個行程看得到的檔案系統。預設是空的（找不到任何檔案），
	// 由呼叫端指定原始資料目錄。
	FS *FileSystem

	// BaseTime 是「行程開始的那一刻」。日期時間一律從它加上 Clock 算出來，
	// 這樣同一份輸入永遠得到同一份輸出。預設取原版 CIV.EXE 的檔案時間。
	BaseTime time.Time

	// Clock 是這個行程看到的時間。預設是 StepClock：跟著指令數走，
	// 單調而且可重現。
	Clock Clock

	// resources 是 FindResource 發出去的 HRSRC 對應表（1-based）。
	resources []ne.Resource

	dialogSeq int
	msgCount  map[uint16]int

	// Sounds 記下播過的音效檔名。
	Sounds []string

	// Libraries 記下 LoadLibrary 過的名字。
	Libraries []string

	// FPMathCodes 記下每次 `WIN87EM.__FPMATH` 的 BX 值。這是「先量再寫」
	// 的欄位：等看到實際用到哪些功能碼，再決定要實作哪些。
	FPMathCodes []uint16

	// StackLimit 是 `InitTask` 回傳的 CX。**這是假說**：Borland 的啟動碼
	// 拿它設堆疊探測的下限，取檔頭的 `ne_stack`（位元組數）在語意上合理，
	// 但沒有對原版量過。如果之後看到堆疊探測誤判，第一個要回頭查的就是它。
	StackLimit uint16
}

// Handler 實作一支 API。
//
// 參數由 Args 提供，回傳值是 **DX:AX**（Win16 的 32 位元回傳慣例；只回
// 16 位元的就讓高位是 0）。要動其他暫存器（`InitTask` 會設 CX／SI／DI／ES）
// 就直接改 `p.CPU`。
//
// **不要自己呼叫 RetFar**：回傳之後由派送端統一用 winapi 表上的參數
// 位元組數彈掉。把「彈幾個 byte」從處理器裡拿掉，就少掉一整類 bug。
type Handler func(p *Process, a Args) (uint32, error)

// RawHandler 是不走 pascal 慣例的入口：參數在暫存器裡，回傳前要自己
// 呼叫 p.CPU.RetFar()。
type RawHandler func(p *Process, imp ne.Import) error

// Args 讀一次呼叫的參數區。
//
// pascal 呼叫慣例由左往右推、堆疊往下長，所以**簽章最左邊**的參數在最高位址。
// 這裡的 off 一律是「從簽章左邊算起的位元組位移」，和 windows.h 的宣告
// 逐字對得起來：`BitBlt(HDC, int, int, int, int, HDC, int, int, DWORD)`
// 的第一個 HDC 是 Word(0)、最後的 DWORD 是 Long(18)。
type Args struct {
	p   *Process
	top uint16 // 第一個參數的**後面**一個位址
}

// Word 取一個 16 位元參數。
func (a Args) Word(off int) uint16 {
	v, _ := a.p.Mod.Mem.ReadU16(a.p.CPU.Seg[cpu.SS], a.top-uint16(off)-2)
	return v
}

// Long 取一個 32 位元參數（低位字在低位址）。
func (a Args) Long(off int) uint32 {
	lo, _ := a.p.Mod.Mem.ReadU16(a.p.CPU.Seg[cpu.SS], a.top-uint16(off)-4)
	hi, _ := a.p.Mod.Mem.ReadU16(a.p.CPU.Seg[cpu.SS], a.top-uint16(off)-2)
	return uint32(hi)<<16 | uint32(lo)
}

// Ptr 取一個 far 指標，回 (selector, 位移)。
func (a Args) Ptr(off int) (sel, o uint16) {
	v := a.Long(off)
	return uint16(v >> 16), uint16(v)
}

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
		winapi.Describe(e.Import.Key()), e.Call.FromCS, e.Call.FromIP, e.Call.Steps)
}

// NewProcess 建立行程並把暫存器設成 NE 進入點的初始狀態。
//
// 初始狀態的來源全部是檔頭：CS:IP 與 SS:SP 直接取 `ne_csip`／`ne_sssp`，
// DS 取自動資料段。`ne_sssp` 的兩種特例（段號 0、SP 0）照 Windows 載入器
// 的規則補：段號 0 表示堆疊就在 DGROUP 裡，SP 0 表示指到那塊的尾巴。
func NewProcess(mod *Module) (*Process, error) {
	p := &Process{Mod: mod, Handlers: map[string]Handler{}, RawHandlers: map[string]RawHandler{}, TraceLimit: 100000}
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
	c.Seg[cpu.SS] = ssSel
	c.SetR16(cpu.SP, sp)

	// PSP：真 Windows 給每個任務一個 DOS 程式段前綴，命令列在 +0x80
	// （長度位元組 ＋ 內文 ＋ CR）。這裡只需要「命令列是空的」。
	psp := mod.Mem.Alloc("PSP", 0x100)
	psp.Data[0x80] = 0
	psp.Data[0x81] = 0x0D
	p.PSP = psp.Sel
	p.StackLimit = mod.Image.StackSize

	// DOS 環境區塊：一串 `NAME=VALUE\0`、以空字串收尾，接一個計數字，
	// 再接執行檔完整路徑。Borland 的啟動碼會走完整串找那個路徑。
	p.ModulePath = `C:\CIV\CIV.EXE`
	var env []byte
	env = append(env, []byte("PATH=C:\\\x00")...)
	env = append(env, 0x00)       // 環境結束
	env = append(env, 0x01, 0x00) // 後面還有一個字串
	env = append(env, []byte(p.ModulePath)...)
	env = append(env, 0x00)
	envBlk := mod.Mem.Alloc("DOS 環境", len(env)+16)
	copy(envBlk.Data, env)
	p.Env = envBlk.Sel

	p.CurrentDrive, p.CurrentDir = 2, `CIV`
	p.FS = NewFileSystem("", "CIV")
	p.ScreenW, p.ScreenH = 640, 480
	p.Screen = NewSurface(p.ScreenW, p.ScreenH)
	p.Metrics = defaultMetrics(p.ScreenW, p.ScreenH)
	p.initPalette()
	p.Objects = NewObjects()
	p.Classes = map[string]*Class{}
	p.Windows = map[uint16]*Window{}
	p.nextHWnd = 0x0800
	p.nextTimerID = 0x8000

	// 桌面視窗：GetDesktopWindow 要回一個**真的視窗**。回 0 的話，
	// 呼叫端拿它去 GetWindowRect 會拿不到值，而 RECT 是呼叫端的區域變數
	// ——裡面是垃圾，於是對話框會被擺到螢幕外。這是那種「回 0 看起來
	// 沒事」的錯誤。
	desktop := &Window{
		Handle: p.nextHWnd, Visible: true, Enabled: true,
		W: p.ScreenW, H: p.ScreenH,
		ClientW: p.ScreenW, ClientH: p.ScreenH,
		ClassName: "#32769",
	}
	p.nextHWnd++
	p.Desktop = desktop.Handle
	p.Windows[desktop.Handle] = desktop
	p.CallStepLimit = 50_000_000
	p.BaseTime = time.Date(1993, 12, 14, 15, 19, 0, 0, time.UTC)
	p.Clock = &StepClock{CPU: c}
	c.OnFarCall = p.onFarCall
	c.OnInt = p.onInt
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
	fromIP, _ := p.Mod.Mem.ReadU16(c.Seg[cpu.SS], c.R16(cpu.SP))
	fromCS, _ := p.Mod.Mem.ReadU16(c.Seg[cpu.SS], c.R16(cpu.SP)+2)
	call := Call{Import: imp, Steps: c.Steps, FromCS: fromCS, FromIP: fromIP}
	// Trace 有上限（長跑會吃光記憶體），但**計數沒有**：
	// 上限一到就只剩前面那段，用它下「遊戲不再呼叫某支 API」的結論會錯。
	if p.callCount == nil {
		p.callCount = map[string]int{}
	}
	p.callCount[imp.Key()]++
	p.Calls++
	p.LastCall = call
	if p.TraceLimit == 0 || len(p.Trace) < p.TraceLimit {
		p.Trace = append(p.Trace, call)
	} else if len(p.Recent) < 64 {
		p.Recent = append(p.Recent, call)
	} else {
		copy(p.Recent, p.Recent[1:])
		p.Recent[len(p.Recent)-1] = call
	}

	key := imp.Key()
	if raw, ok := p.RawHandlers[key]; ok {
		return true, raw(p, imp)
	}
	h, ok := p.Handlers[key]
	if !ok {
		return true, &UnhandledAPIError{Import: imp, Call: call}
	}
	fn, ok := winapi.Lookup(key)
	if !ok || fn.ArgBytes < 0 {
		return true, fmt.Errorf("win16: %s 有處理器，但 winapi 表上沒有可用的參數位元組數", key)
	}
	ret, err := h(p, Args{p: p, top: c.R16(cpu.SP) + 4 + uint16(fn.ArgBytes)})
	if err != nil {
		return true, err
	}
	c.SetR16(cpu.AX, uint16(ret))
	c.SetR16(cpu.DX, uint16(ret>>16))
	return true, c.RetFar(uint16(fn.ArgBytes))
}

// CallCount 回傳每一支 API 被呼叫的總次數（沒有上限）。
func (p *Process) CallCount() map[string]int { return p.callCount }

// Run 跑到 Halt、錯誤或步數上限。
func (p *Process) Run(maxSteps uint64) error { return p.CPU.Run(maxSteps) }
