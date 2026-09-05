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

	// 視窗矩形與客戶區矩形，都是螢幕座標，右下不含。
	X, Y, W, H       int
	ClientX, ClientY int
	ClientW, ClientH int

	Visible bool
	Enabled bool
	HasMenu bool

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
func (p *Process) frameFor(style uint32, hasMenu bool) (l, t, r, b int) {
	sm := p.Metrics
	switch {
	case style&WSThickFram != 0:
		l, t, r, b = sm[32], sm[33], sm[32], sm[33]
	case style&WSDlgFrame != 0:
		l, t, r, b = sm[7], sm[8], sm[7], sm[8]
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

// layout 依視窗矩形與樣式算出客戶區。
func (p *Process) layout(w *Window) {
	l, t, r, b := p.frameFor(w.Style, w.HasMenu)
	w.ClientX, w.ClientY = w.X+l, w.Y+t
	w.ClientW, w.ClientH = w.W-l-r, w.H-t-b
	if w.ClientW < 0 {
		w.ClientW = 0
	}
	if w.ClientH < 0 {
		w.ClientH = 0
	}
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

// SendMessage 直接呼叫視窗程序（不進佇列）。
func (p *Process) SendMessage(hwnd, msg, wParam uint16, lParam uint32) (uint32, error) {
	w, ok := p.Windows[hwnd]
	if !ok {
		return 0, nil
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

// nextMessage 取下一則要處理的訊息。
//
// 順序照 Windows 的規則：佇列 → 計時器 → 重畫。**重畫排最後**才對——
// 不然一則 WM_PAINT 會一直插隊，輸入永遠輪不到。
func (p *Process) nextMessage() (Msg, bool) {
	if len(p.Queue) > 0 {
		m := p.Queue[0]
		p.Queue = p.Queue[1:]
		return m, true
	}
	now := p.Clock.Millis()
	for i := range p.Timers {
		t := &p.Timers[i]
		if now >= t.NextDue {
			t.NextDue = now + t.Elapse
			return Msg{HWnd: t.HWnd, Message: WMTimer, WParam: t.ID, Time: now}, true
		}
	}
	for _, h := range p.WindowOrder {
		w := p.Windows[h]
		if w != nil && w.Visible && w.NeedPaint {
			return Msg{HWnd: h, Message: WMPaint, Time: now}, true
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
