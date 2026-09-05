package win16

// 視窗訊息（只列這一層用得到的）。
const (
	WMCreate      = 0x0001
	WMDestroy     = 0x0002
	WMMove        = 0x0003
	WMSize        = 0x0005
	WMActivate    = 0x0006
	WMSetFocus    = 0x0007
	WMPaint       = 0x000F
	WMClose       = 0x0010
	WMQuit        = 0x0012
	WMEraseBkgnd  = 0x0014
	WMShowWindow  = 0x0018
	WMActivateApp = 0x001C
	WMNCCreate    = 0x0081
	WMNCPaint     = 0x0085
	WMTimer       = 0x0113
	WMQueryNewPal = 0x030F
	WMPaletteChg  = 0x0311
)

// 視窗樣式（只列會影響版面的）。
const (
	WSChild     = 0x40000000
	WSVisible   = 0x10000000
	WSCaption   = 0x00C00000
	WSVScroll   = 0x00200000
	WSHScroll   = 0x00100000
	WSBorder    = 0x00800000
	WSDlgFrame  = 0x00400000
	WSThickFram = 0x00040000
	WSPopup     = 0x80000000
)

// CWUseDefault 是 CreateWindow 的「你決定」。
const CWUseDefault = 0x8000

// Class 是一個註冊過的視窗類別。
type Class struct {
	Name       string
	Style      uint16
	ProcSel    uint16
	ProcOff    uint16
	ClsExtra   int
	WndExtra   int
	Instance   uint16
	Icon       uint16
	Cursor     uint16
	Background uint16
	MenuName   string
	Extra      []byte
}

// Window 是一個視窗。
type Window struct {
	Handle   uint16
	Class    *Class
	Text     string
	Style    uint32
	Parent   uint16
	Menu     uint16
	Instance uint16
	ProcSel  uint16
	ProcOff  uint16
	Extra    []byte

	// X／Y 是**相對父視窗客戶區**的位置（頂層視窗則是螢幕座標），
	// W／H 是整個視窗的大小。這和 Windows 一致：父視窗一移動，
	// 子視窗跟著走而不必逐一改座標。
	X, Y, W, H int

	// AbsX／AbsY 與 ClientX／ClientY 是換算出來的**螢幕座標**，
	// 由 layout 維護。命中測試與畫圖一律用這一組。
	AbsX, AbsY       int
	ClientX, ClientY int
	ClientW, ClientH int

	Visible bool
	Enabled bool
	HasMenu bool

	// 對話框與控制項
	IsDialog  bool
	CtrlID    uint16
	ClassName string
	Children  []uint16
	Checked   uint16

	// 捲軸狀態，索引 0 是水平（SB_HORZ）、1 是垂直（SB_VERT）。
	ScrollPos [2]int
	ScrollMin [2]int
	ScrollMax [2]int

	// DlgProc 是對話框範本指定的 DLGPROC；由 DefDlgProc 呼叫。
	DlgProcSel uint16
	DlgProcOff uint16
	inDlgProc  bool

	// GoProc 非 nil 時，訊息由 Go 這一側處理（內建控制項用）。
	GoProc func(w *Window, msg, wParam uint16, lParam uint32) (uint32, error)

	// 無效區域（客戶座標，右下不含）。空的時候 NeedPaint 是 false。
	NeedPaint  bool
	InvL, InvT int
	InvR, InvB int
	EraseBkg   bool
}

// Msg 是一則訊息。
type Msg struct {
	HWnd    uint16
	Message uint16
	WParam  uint16
	LParam  uint32
	Time    uint32
	PtX     int16
	PtY     int16
}

// Timer 是一個 SetTimer 建的計時器。
type Timer struct {
	HWnd    uint16
	ID      uint16
	Elapse  uint32 // 毫秒
	ProcSel uint16
	ProcOff uint16
	NextDue uint32
}

// 系統度量的預設值（Windows 3.1、VGA 640×480）。
//
// **這是假說**：值取自 Windows 3.1 在 VGA 上的常見設定，沒有對原版量過。
// 它們決定客戶區大小，也就決定地圖從哪一個像素開始畫——所以在 M4 逐點
// 比對之前必須逐項核對（spec 004 §6）。
func defaultMetrics(screenW, screenH int) map[int]int {
	m := map[int]int{
		0:  screenW, // SM_CXSCREEN
		1:  screenH, // SM_CYSCREEN
		2:  16,      // SM_CXVSCROLL
		3:  16,      // SM_CYHSCROLL
		4:  19,      // SM_CYCAPTION
		5:  1,       // SM_CXBORDER
		6:  1,       // SM_CYBORDER
		7:  3,       // SM_CXDLGFRAME
		8:  3,       // SM_CYDLGFRAME
		9:  16,      // SM_CYVTHUMB
		10: 16,      // SM_CXHTHUMB
		11: 32,      // SM_CXICON
		12: 32,      // SM_CYICON
		13: 32,      // SM_CXCURSOR
		14: 32,      // SM_CYCURSOR
		15: 19,      // SM_CYMENU
		19: 1,       // SM_MOUSEPRESENT
		20: 16,      // SM_CYVSCROLL
		21: 16,      // SM_CXHSCROLL
		32: 4,       // SM_CXFRAME ＝ CXDLGFRAME ＋ CXBORDER
		33: 4,       // SM_CYFRAME
	}
	m[16] = screenW - 2*m[32]        // SM_CXFULLSCREEN
	m[17] = screenH - 2*m[33] - m[4] // SM_CYFULLSCREEN
	return m
}

// frameFor 依樣式算出非客戶區在四邊各佔幾個像素。
//
// **邊框保留的是 `SM_CXFRAME`（4），不是 `SM_CXDLGFRAME`（3）。**
// 這一項曾經寫成 3，理由是「量到的客戶區是 162×102」——那是**量錯了**：
// 參考幀上框的最內側那一道線是黑的，而未揭露的地圖格也是黑的，量黑區
// 的時候分不出來（civ1 的 DS327 §3 用同一張幀逐點掃過，結論相同）。
//
// 用 4 之後三件事同時對上：
//
//   - 小地圖視窗：遊戲要 160×100 的緩衝區，開出來的視窗是 168×127，
//     客戶區 168 − 2×4 ＝ **160**，和緩衝區寬度一模一樣。
//   - 主視窗：(168,0) 632×600 → 客戶區原點 (172,42)，正是參考幀上
//     tile 內容的起點（DS327 §1／§2：離屏 bitmap 的 (0,0) 對應
//     client 的 (0,0)，所以兩者是同一點）。
//   - 客戶區寬 632 − 4 − 4 − 16 ＝ **608** ＝ 19 格 × 32，與遊戲自己
//     算出來的視窗寬度（608 ＋ 2×4 ＋ 16 捲軸 ＝ 632）閉合。
func (p *Process) frameFor(style uint32, hasMenu bool) (l, t, r, b int) {
	sm := p.Metrics
	switch {
	case style&WSThickFram != 0:
		l, t, r, b = sm[32], sm[33], sm[32], sm[33]
	case style&WSDlgFrame != 0:
		l, t, r, b = sm[32], sm[33], sm[32], sm[33]
	case style&WSBorder != 0:
		l, t, r, b = sm[5], sm[6], sm[5], sm[6]
	}
	if style&WSCaption == WSCaption {
		t += sm[4]
	}
	if hasMenu {
		t += sm[15]
	}
	if style&WSVScroll != 0 {
		r += sm[2]
	}
	if style&WSHScroll != 0 {
		b += sm[3]
	}
	return
}

// layout 算出這個視窗（與它所有子視窗）的螢幕座標與客戶區。
//
// 子視窗的座標是相對父視窗客戶區的，所以父視窗一動就要整棵重算——
// 不重算的話，被移動過的對話框上的按鈕會留在原地，而且是留在螢幕外。
func (p *Process) layout(w *Window) {
	// 只有 WS_CHILD 的位置是相對父視窗的。彈出式視窗（含對話框）
	// 雖然也有 owner，但位置是螢幕座標——owner 移動不會帶著它跑。
	ox, oy := 0, 0
	if w.Style&WSChild != 0 && w.Parent != 0 {
		if pw, ok := p.Windows[w.Parent]; ok {
			ox, oy = pw.ClientX, pw.ClientY
		}
	}
	w.AbsX, w.AbsY = ox+w.X, oy+w.Y

	l, t, r, b := 0, 0, 0, 0
	if w.Style&WSChild == 0 {
		l, t, r, b = p.frameFor(w.Style, w.HasMenu)
	} else {
		// 子視窗只吃邊框，不吃標題列與選單列。
		l, t, r, b = p.childFrameFor(w.Style)
	}
	w.ClientX, w.ClientY = w.AbsX+l, w.AbsY+t
	w.ClientW, w.ClientH = w.W-l-r, w.H-t-b
	if w.ClientW < 0 {
		w.ClientW = 0
	}
	if w.ClientH < 0 {
		w.ClientH = 0
	}
	for _, c := range w.Children {
		if cw, ok := p.Windows[c]; ok {
			p.layout(cw)
		}
	}
}

// childFrameFor 是子視窗的非客戶區。子視窗沒有標題列——CIV.EXE 的
// 選單項目是 WS_CHILD 加上 WS_BORDER 的自繪控制項，照頂層規則算的話
// 每一個都會被吃掉 19 個像素的「標題列」，客戶區高度變成 0。
func (p *Process) childFrameFor(style uint32) (l, t, r, b int) {
	sm := p.Metrics
	switch {
	case style&WSThickFram != 0, style&WSDlgFrame != 0:
		l, t, r, b = sm[7], sm[8], sm[7], sm[8]
	case style&WSBorder != 0:
		l, t, r, b = sm[5], sm[6], sm[5], sm[6]
	}
	if style&WSVScroll != 0 {
		r += sm[2]
	}
	if style&WSHScroll != 0 {
		b += sm[3]
	}
	return
}

// Window 取一個視窗。
func (p *Process) Window(h uint16) (*Window, bool) {
	w, ok := p.Windows[h]
	return w, ok
}

// Invalidate 把客戶座標的一塊標成要重畫；nil 表示整個客戶區。
func (p *Process) Invalidate(w *Window, rect *[4]int, erase bool) {
	l, t, r, b := 0, 0, w.ClientW, w.ClientH
	if rect != nil {
		l, t, r, b = rect[0], rect[1], rect[2], rect[3]
	}
	if r <= l || b <= t {
		return
	}
	if !w.NeedPaint {
		w.InvL, w.InvT, w.InvR, w.InvB = l, t, r, b
		w.NeedPaint = true
	} else {
		w.InvL = min(w.InvL, l)
		w.InvT = min(w.InvT, t)
		w.InvR = max(w.InvR, r)
		w.InvB = max(w.InvB, b)
	}
	if erase {
		w.EraseBkg = true
	}
}

// Validate 把客戶座標的一塊標成畫好了。
//
// 只處理「整塊蓋掉」的情形——GDI 的無效區域是任意區域，這裡只留一個
// 外接矩形。多畫幾個像素不會讓畫面錯，少畫才會，所以取聯集是安全的近似。
func (p *Process) Validate(w *Window, rect *[4]int) {
	if rect == nil {
		w.NeedPaint, w.EraseBkg = false, false
		return
	}
	if rect[0] <= w.InvL && rect[1] <= w.InvT && rect[2] >= w.InvR && rect[3] >= w.InvB {
		w.NeedPaint, w.EraseBkg = false, false
	}
}

// MsgLogEntry 是一則訊息的紀錄。
type MsgLogEntry struct {
	HWnd    uint16
	Message uint16
	WParam  uint16
	LParam  uint32
	Steps   uint64
}

// logMsg 把訊息記進環狀緩衝。**只留最後 N 則**——長跑會送出上百萬則，
// 全留會把記憶體吃光，而卡住的原因一定在最後那幾則裡。
func (p *Process) logMsg(hwnd, msg, wParam uint16, lParam uint32) {
	if p.MsgLogSize == 0 {
		return
	}
	e := MsgLogEntry{HWnd: hwnd, Message: msg, WParam: wParam, LParam: lParam, Steps: p.CPU.Steps}
	if len(p.MsgLog) < p.MsgLogSize {
		p.MsgLog = append(p.MsgLog, e)
		return
	}
	copy(p.MsgLog, p.MsgLog[1:])
	p.MsgLog[len(p.MsgLog)-1] = e
}

// MsgName 把訊息編號翻成名字（只涵蓋這一層會遇到的）。
func MsgName(m uint16) string {
	if n, ok := msgNames[m]; ok {
		return n
	}
	return ""
}

var msgNames = map[uint16]string{
	0x0001: "WM_CREATE", 0x0002: "WM_DESTROY", 0x0003: "WM_MOVE",
	0x0005: "WM_SIZE", 0x0006: "WM_ACTIVATE", 0x0007: "WM_SETFOCUS",
	0x0008: "WM_KILLFOCUS", 0x000A: "WM_ENABLE", 0x000C: "WM_SETTEXT",
	0x000D: "WM_GETTEXT", 0x000F: "WM_PAINT", 0x0010: "WM_CLOSE",
	0x0011: "WM_QUERYENDSESSION", 0x0012: "WM_QUIT", 0x0014: "WM_ERASEBKGND",
	0x0018: "WM_SHOWWINDOW", 0x001C: "WM_ACTIVATEAPP", 0x001D: "WM_FONTCHANGE",
	0x0020: "WM_SETCURSOR", 0x0021: "WM_MOUSEACTIVATE", 0x0024: "WM_GETMINMAXINFO",
	0x0030: "WM_SETFONT", 0x0081: "WM_NCCREATE", 0x0082: "WM_NCDESTROY",
	0x0083: "WM_NCCALCSIZE", 0x0084: "WM_NCHITTEST", 0x0085: "WM_NCPAINT",
	0x0086: "WM_NCACTIVATE", 0x0100: "WM_KEYDOWN", 0x0101: "WM_KEYUP",
	0x0102: "WM_CHAR", 0x0110: "WM_INITDIALOG", 0x0111: "WM_COMMAND",
	0x0112: "WM_SYSCOMMAND", 0x0113: "WM_TIMER", 0x0114: "WM_HSCROLL",
	0x0115: "WM_VSCROLL", 0x0200: "WM_MOUSEMOVE", 0x0201: "WM_LBUTTONDOWN",
	0x0202: "WM_LBUTTONUP", 0x0203: "WM_LBUTTONDBLCLK", 0x0204: "WM_RBUTTONDOWN",
	0x0205: "WM_RBUTTONUP", 0x030F: "WM_QUERYNEWPALETTE", 0x0311: "WM_PALETTECHANGED",
	0x00F0: "BM_GETCHECK", 0x00F1: "BM_SETCHECK",
}

// MsgCount 統計每個訊息被送過幾次。畫面沒動的時候，這是最快看出
// 「到底在跑什麼」的東西。
func (p *Process) MsgCount() map[uint16]int { return p.msgCount }

// SendMessage 直接呼叫視窗程序（不進佇列）。
func (p *Process) SendMessage(hwnd, msg, wParam uint16, lParam uint32) (uint32, error) {
	if p.msgCount == nil {
		p.msgCount = map[uint16]int{}
	}
	p.msgCount[msg]++
	p.logMsg(hwnd, msg, wParam, lParam)
	w, ok := p.Windows[hwnd]
	if !ok {
		return 0, nil
	}
	if w.GoProc != nil {
		return w.GoProc(w, msg, wParam, lParam)
	}
	return p.Call16(w.ProcSel, w.ProcOff,
		hwnd, msg, wParam, uint16(lParam>>16), uint16(lParam))
}

// PostMessage 把訊息排進佇列。
func (p *Process) PostMessage(hwnd, msg, wParam uint16, lParam uint32) {
	p.Queue = append(p.Queue, Msg{
		HWnd: hwnd, Message: msg, WParam: wParam, LParam: lParam,
		Time: p.Clock.Millis(),
	})
}

// MsgFilter 是 PeekMessage／GetMessage 的過濾條件。
//
// **這不是可以省略的細節。** CIV.EXE 有一個只 peek 鍵盤訊息
// （`0x0100..0x0108`）的輪詢迴圈，和一個處理全部訊息的主迴圈。不看過濾
// 條件的話，鍵盤迴圈會把計時器訊息一則一則吃掉並當成按鍵，而主迴圈
// 永遠等不到它要的東西——症狀是「按了按鈕之後畫面再也不動」。
type MsgFilter struct {
	HWnd     uint16 // 0 表示不限視窗
	Min, Max uint16 // 都是 0 表示不限訊息
}

func (f MsgFilter) match(m Msg) bool {
	if f.HWnd != 0 && m.HWnd != f.HWnd {
		return false
	}
	if f.Min == 0 && f.Max == 0 {
		return true
	}
	return m.Message >= f.Min && m.Message <= f.Max
}

// nextMessage 取下一則符合過濾條件的訊息。
//
// 順序照 Windows 的規則：佇列 → 計時器 → 重畫。**重畫排最後**才對——
// 不然一則 WM_PAINT 會一直插隊，輸入永遠輪不到。
//
// 不符合條件的訊息要**留在佇列裡**，不能順手丟掉：那是另一個迴圈在等的。
func (p *Process) nextMessage(f MsgFilter, remove bool) (Msg, bool) {
	for i, m := range p.Queue {
		if !f.match(m) {
			continue
		}
		if remove {
			p.Queue = append(p.Queue[:i], p.Queue[i+1:]...)
		}
		return m, true
	}
	now := p.Clock.Millis()
	for i := range p.Timers {
		t := &p.Timers[i]
		if now < t.NextDue {
			continue
		}
		m := Msg{HWnd: t.HWnd, Message: WMTimer, WParam: t.ID, Time: now}
		if !f.match(m) {
			continue
		}
		if remove {
			t.NextDue = now + t.Elapse
		}
		return m, true
	}
	for _, h := range p.WindowOrder {
		w := p.Windows[h]
		if w == nil || !w.Visible || !w.NeedPaint {
			continue
		}
		m := Msg{HWnd: h, Message: WMPaint, Time: now}
		if f.match(m) {
			return m, true
		}
	}
	return Msg{}, false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
