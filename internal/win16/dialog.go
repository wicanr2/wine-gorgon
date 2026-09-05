package win16

import (
	"fmt"

	"github.com/wicanr2/wine-gorgon/internal/ne"
)

// Win16 的對話框範本是一串**變長欄位**：固定頭之後接選單名、類別名、
// 標題，每一項都可能是字串或「0xFF ＋ 編號」。控制項也是同樣的形狀。
// 解析它不能用固定位移，只能一路往前走——這就是下面這個 reader 存在的
// 理由。

type tmplReader struct {
	p    *Process
	sel  uint16
	off  uint16
	fail bool
}

func (r *tmplReader) u8() uint8 {
	v, err := r.p.Mod.Mem.ReadU8(r.sel, r.off)
	if err != nil {
		r.fail = true
		return 0
	}
	r.off++
	return v
}

func (r *tmplReader) u16() uint16 {
	lo, hi := r.u8(), r.u8()
	return uint16(hi)<<8 | uint16(lo)
}

func (r *tmplReader) u32() uint32 {
	lo, hi := r.u16(), r.u16()
	return uint32(hi)<<16 | uint32(lo)
}

func (r *tmplReader) str() string {
	var b []byte
	for i := 0; i < 256; i++ {
		c := r.u8()
		if c == 0 || r.fail {
			break
		}
		b = append(b, c)
	}
	return string(b)
}

// nameOrOrdinal 讀「字串，或 0xFF 後面接一個 16 位元編號」。
func (r *tmplReader) nameOrOrdinal() (string, uint16) {
	c, err := r.p.Mod.Mem.ReadU8(r.sel, r.off)
	if err != nil {
		r.fail = true
		return "", 0
	}
	if c == 0xFF {
		r.off++
		return "", r.u16()
	}
	return r.str(), 0
}

// 對話框範本裡的內建控制項編號。
var builtinClass = map[uint8]string{
	0x80: "BUTTON", 0x81: "EDIT", 0x82: "STATIC",
	0x83: "LISTBOX", 0x84: "SCROLLBAR", 0x85: "COMBOBOX",
}

// DialogItem 是一個控制項的範本。
type DialogItem struct {
	X, Y, CX, CY int
	ID           uint16
	Style        uint32
	Class        string
	Text         string
}

// DialogTemplate 是解好的對話框範本。
type DialogTemplate struct {
	Style        uint32
	X, Y, CX, CY int
	MenuName     string
	ClassName    string
	Caption      string
	FontSize     uint16
	FontFace     string
	Items        []DialogItem
}

// parseDialogTemplate 解一份對話框範本。
func (p *Process) parseDialogTemplate(sel, off uint16) (*DialogTemplate, error) {
	r := &tmplReader{p: p, sel: sel, off: off}
	t := &DialogTemplate{}
	t.Style = r.u32()
	n := int(r.u8())
	t.X, t.Y = int(int16(r.u16())), int(int16(r.u16()))
	t.CX, t.CY = int(int16(r.u16())), int(int16(r.u16()))
	t.MenuName, _ = r.nameOrOrdinal()
	t.ClassName, _ = r.nameOrOrdinal()
	t.Caption = r.str()
	const dsSetFont = 0x40
	if t.Style&dsSetFont != 0 {
		t.FontSize = r.u16()
		t.FontFace = r.str()
	}
	for i := 0; i < n; i++ {
		var it DialogItem
		it.X, it.Y = int(int16(r.u16())), int(int16(r.u16()))
		it.CX, it.CY = int(int16(r.u16())), int(int16(r.u16()))
		it.ID = r.u16()
		it.Style = r.u32()
		c, err := p.Mod.Mem.ReadU8(sel, r.off)
		if err != nil {
			return nil, fmt.Errorf("win16: 對話框範本第 %d 項讀不到類別", i)
		}
		if c&0x80 != 0 {
			r.off++
			name, ok := builtinClass[c]
			if !ok {
				return nil, fmt.Errorf("win16: 對話框範本用了未知的內建類別 %02X", c)
			}
			it.Class = name
		} else {
			it.Class = r.str()
		}
		text, ord := r.nameOrOrdinal()
		if text == "" && ord != 0 {
			text = fmt.Sprintf("#%d", ord)
		}
		it.Text = text
		r.off += uint16(r.u8()) // cbCreateParams：後面那幾個 byte 跳過
		if r.fail {
			return nil, fmt.Errorf("win16: 對話框範本第 %d 項讀到區塊尾端", i)
		}
		t.Items = append(t.Items, it)
	}
	return t, nil
}

// dlgToPixels 把對話框單位換成像素。
//
// 水平是 1/4 個平均字寬、垂直是 1/8 個字高（GetDialogBaseUnits 的定義）。
// 這個換算直接決定控制項落在哪個像素，所以基準單位是**假說**時，
// 對話框的版面也只能算假說（spec 004 §6）。
func (p *Process) dlgToPixels(t *DialogTemplate, x, y int) (int, int) {
	bx, by := 8, 16
	if v, ok := p.Metrics[-1]; ok {
		bx = v
	}
	return x * bx / 4, y * by / 8
}

// RegisterDialog 登記對話框相關的 API。
func RegisterDialog(p *Process) {
	h := p.Handlers

	// CreateDialog(HINSTANCE, LPCSTR template, HWND parent, DLGPROC)
	h["USER.#89"] = func(p *Process, a Args) (uint32, error) {
		sel, off := a.Ptr(2)
		id, name := p.resName(sel, off)
		r, ok := p.Mod.Image.FindResource(ne.RTDialog, "", name, id)
		if !ok {
			p.note("CreateDialog 找不到對話框範本 %s/%d", name, id)
			return 0, nil
		}
		data, err := p.Mod.Image.ResourceData(r)
		if err != nil {
			return 0, err
		}
		blk := p.Mod.Mem.Alloc("對話框範本 "+r.String(), len(data))
		copy(blk.Data, data)
		defer p.Mod.Mem.Free(blk.Sel)
		procSel, procOff := a.Ptr(8)
		return p.createDialog(blk.Sel, 0, a.Word(6), procSel, procOff, 0)
	}

	// CreateDialogIndirect(HINSTANCE, const void far*, HWND, DLGPROC)
	h["USER.#219"] = func(p *Process, a Args) (uint32, error) {
		sel, off := a.Ptr(2)
		procSel, procOff := a.Ptr(8)
		return p.createDialog(sel, off, a.Word(6), procSel, procOff, 0)
	}

	h["USER.#91"] = func(p *Process, a Args) (uint32, error) { // GetDlgItem
		return uint32(p.dlgItem(a.Word(0), a.Word(2))), nil
	}

	h["USER.#277"] = func(p *Process, a Args) (uint32, error) { // GetDlgCtrlID
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		return uint32(w.CtrlID), nil
	}

	h["USER.#101"] = func(p *Process, a Args) (uint32, error) { // SendDlgItemMessage
		hchild := p.dlgItem(a.Word(0), a.Word(2))
		if hchild == 0 {
			return 0, nil
		}
		return p.SendMessage(hchild, a.Word(4), a.Word(6), a.Long(8))
	}

	h["USER.#90"] = func(p *Process, _ Args) (uint32, error) { return 0, nil } // IsDialogMessage
}

// createDialog 依範本建對話框與它的控制項。
func (p *Process) createDialog(sel, off, parent, procSel, procOff uint16, param uint32) (uint32, error) {
	t, err := p.parseDialogTemplate(sel, off)
	if err != nil {
		return 0, err
	}

	p.dialogSeq++
	p.note("對話框 #%d 範本：類別=%q 標題=%q 樣式=%08X (%d,%d %dx%d) %d 項",
		p.dialogSeq, t.ClassName, t.Caption, t.Style, t.X, t.Y, t.CX, t.CY, len(t.Items))
	x, y := p.dlgToPixels(t, t.X, t.Y)
	// 範本座標是相對 owner 客戶區的（沒有 DS_ABSALIGN 時）。對話框本身
	// 是彈出式視窗，所以在這裡就換成螢幕座標，之後 owner 移動不影響它。
	if t.Style&WSChild == 0 {
		if ow, ok := p.Window(parent); ok {
			x += ow.ClientX
			y += ow.ClientY
		}
	}
	cx, cy := p.dlgToPixels(t, t.CX, t.CY)
	l, tp, r, b := p.frameFor(t.Style, t.MenuName != "")
	w := &Window{
		Text: t.Caption, Style: t.Style, Parent: parent,
		ProcSel: procSel, ProcOff: procOff,
		X: x, Y: y, W: cx + l + r, H: cy + tp + b,
		Enabled: true, IsDialog: true,
		DlgProcSel: procSel, DlgProcOff: procOff,
		Extra: make([]byte, 30), // DLGWINDOWEXTRA
	}
	// 對話框範本指定了自己的類別時，**視窗程序是那個類別的**，
	// DLGPROC 另外存起來由 DefDlgProc 呼叫。CIV.EXE 的對話框全部用
	// 自己註冊的 `CIVDIALOG`——把 DLGPROC 直接當成視窗程序的話，
	// 類別程序永遠不會跑，畫面上就什麼都不會出現。
	if t.ClassName != "" {
		if cls, ok := p.Classes[upper(t.ClassName)]; ok {
			w.Class = cls
			w.ProcSel, w.ProcOff = cls.ProcSel, cls.ProcOff
			if cls.WndExtra > len(w.Extra) {
				w.Extra = make([]byte, cls.WndExtra)
			}
		} else {
			p.note("對話框類別 %q 沒註冊過，退回直接用 DLGPROC", t.ClassName)
		}
	}
	w.Handle = p.nextHWnd
	p.nextHWnd++
	p.Windows[w.Handle] = w
	p.WindowOrder = append(p.WindowOrder, w.Handle)
	p.layout(w)

	for _, it := range t.Items {
		ix, iy := p.dlgToPixels(t, it.X, it.Y)
		iw, ih := p.dlgToPixels(t, it.CX, it.CY)
		child := &Window{
			Text: it.Text, Style: it.Style | WSChild, Parent: w.Handle,
			CtrlID: it.ID, ClassName: it.Class,
			X: ix, Y: iy, W: iw, H: ih,
			Enabled: it.Style&0x08000000 == 0, // WS_DISABLED
			Visible: it.Style&WSVisible != 0,
		}
		child.Handle = p.nextHWnd
		p.nextHWnd++
		// 內建控制項的視窗程序在 Go 這一側：它們的行為（回傳值、
		// 送出 WM_COMMAND）遊戲會用到，外觀在這一層還沒做。
		child.GoProc = p.controlProc
		p.Windows[child.Handle] = child
		p.WindowOrder = append(p.WindowOrder, child.Handle)
		p.layout(child)
		w.Children = append(w.Children, child.Handle)
	}

	// WM_INITDIALOG 的 wParam 是第一個控制項的 handle；回非零表示
	// 「請系統設定焦點」。
	first := uint16(0)
	if len(w.Children) > 0 {
		first = w.Children[0]
	}
	const wmInitDialog = 0x0110
	if _, err := p.SendMessage(w.Handle, wmInitDialog, first, param); err != nil {
		return 0, err
	}
	if t.Style&WSVisible != 0 {
		if err := p.showWindow(w, 5); err != nil {
			return 0, err
		}
	}
	return uint32(w.Handle), nil
}

// dlgItem 依控制項編號找子視窗。
func (p *Process) dlgItem(hdlg, id uint16) uint16 {
	w, ok := p.Window(hdlg)
	if !ok {
		return 0
	}
	for _, c := range w.Children {
		if cw, ok := p.Window(c); ok && cw.CtrlID == id {
			return c
		}
	}
	return 0
}

// controlProc 是內建控制項（BUTTON／STATIC／…）的視窗程序。
//
// 只做「遊戲會觀察到的行為」：文字的存取、按鈕的勾選狀態。**不畫外觀**——
// 對話框的外觀要逐點相同得先有原版的字型與框線規則，那是獨立的一塊。
func (p *Process) controlProc(w *Window, msg, wParam uint16, lParam uint32) (uint32, error) {
	const (
		bmGetCheck = 0x00F0
		bmSetCheck = 0x00F1
		wmSetText  = 0x000C
		wmGetText  = 0x000D
		wmEnable   = 0x000A
	)
	switch msg {
	case WMLButtonDown:
		p.Focus = w.Handle
		return 0, nil
	case WMLButtonUp:
		// 按鈕放開時要通知父視窗，不然對話框上的 OK 按了等於沒按。
		// WM_COMMAND：wParam ＝ 控制項編號，lParam ＝ (通知碼 << 16) | 控制項 handle。
		if w.ClassName == "BUTTON" && w.Parent != 0 {
			const bnClicked = 0
			return p.SendMessage(w.Parent, WMCommand, w.CtrlID,
				uint32(bnClicked)<<16|uint32(w.Handle))
		}
		return 0, nil
	case bmGetCheck:
		return uint32(w.Checked), nil
	case bmSetCheck:
		w.Checked = wParam
		return 0, nil
	case wmSetText:
		w.Text = p.CString(uint16(lParam>>16), uint16(lParam))
		return 1, nil
	case wmEnable:
		w.Enabled = wParam != 0
		return 0, nil
	}
	return 0, nil
}
