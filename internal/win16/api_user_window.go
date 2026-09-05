package win16

import "fmt"

// RegisterUserWindow 登記視窗、訊息與 DC 相關的 USER 處理器。
func RegisterUserWindow(p *Process) {
	h := p.Handlers

	// RegisterClass(const WNDCLASS far*)
	//
	// Win16 的 WNDCLASS 是 26 個 byte：
	//   +0 style(2) +2 lpfnWndProc(4) +6 cbClsExtra(2) +8 cbWndExtra(2)
	//   +10 hInstance(2) +12 hIcon(2) +14 hCursor(2) +16 hbrBackground(2)
	//   +18 lpszMenuName(4) +22 lpszClassName(4)
	h["USER.#57"] = func(p *Process, a Args) (uint32, error) {
		sel, off := a.Ptr(0)
		u16 := func(d uint16) uint16 { v, _ := p.Mod.Mem.ReadU16(sel, off+d); return v }
		far := func(d uint16) (uint16, uint16) { return u16(d + 2), u16(d) }

		nameSel, nameOff := far(22)
		cls := &Class{
			Name:       p.CString(nameSel, nameOff),
			Style:      u16(0),
			ClsExtra:   int(u16(6)),
			WndExtra:   int(u16(8)),
			Instance:   u16(10),
			Icon:       u16(12),
			Cursor:     u16(14),
			Background: u16(16),
		}
		cls.ProcSel, cls.ProcOff = far(2)
		if s, o := far(18); s != 0 || o != 0 {
			cls.MenuName = p.CString(s, o)
		}
		cls.Extra = make([]byte, cls.ClsExtra)
		p.Classes[upper(cls.Name)] = cls
		return 1, nil
	}

	// CreateWindow：參數 30 個 byte，順序見 spec 004 §4 的 Args 慣例。
	h["USER.#41"] = func(p *Process, a Args) (uint32, error) {
		clsSel, clsOff := a.Ptr(0)
		nameSel, nameOff := a.Ptr(4)
		style := a.Long(8)
		x, y := int(int16(a.Word(12))), int(int16(a.Word(14)))
		cw, ch := int(int16(a.Word(16))), int(int16(a.Word(18)))
		parent, menu, inst := a.Word(20), a.Word(22), a.Word(24)
		params := a.Long(26)

		if clsSel == 0 {
			return 0, errUnsupported("CreateWindow 用 atom（%04X）指定類別，還沒做 atom 表", clsOff)
		}
		name := p.CString(clsSel, clsOff)
		cls, ok := p.Classes[upper(name)]
		if !ok {
			p.note("CreateWindow 找不到類別 %q", name)
			return 0, nil
		}

		if uint16(a.Word(12)) == CWUseDefault {
			x, y = 0, 0
		}
		if uint16(a.Word(16)) == CWUseDefault {
			cw, ch = p.ScreenW, p.ScreenH
		}

		w := &Window{
			Class: cls, Text: p.CString(nameSel, nameOff), Style: style,
			Parent: parent, Menu: menu, Instance: inst,
			ProcSel: cls.ProcSel, ProcOff: cls.ProcOff,
			Extra: make([]byte, cls.WndExtra),
			X:     x, Y: y, W: cw, H: ch,
			Enabled: true,
		}
		// 類別帶了選單名稱而且呼叫端沒給 hMenu 時，Windows 會自己載入
		// 那個選單——選單列會吃掉客戶區 SM_CYMENU 的高度，直接影響
		// 地圖從第幾列開始畫。
		w.HasMenu = menu != 0 || (cls.MenuName != "" && style&WSChild == 0)
		w.Handle = p.nextHWnd
		p.nextHWnd++
		p.Windows[w.Handle] = w
		p.WindowOrder = append(p.WindowOrder, w.Handle)
		p.layout(w)

		// CREATESTRUCT 要放在 16 位元看得到的記憶體裡；視窗程序常常
		// 從 lParam 讀 lpCreateParams。
		cs := p.Mod.Mem.Alloc("CREATESTRUCT", 32)
		put := func(d uint16, v uint16) { _ = p.Mod.Mem.WriteU16(cs.Sel, d, v) }
		put(0, uint16(params))
		put(2, uint16(params>>16))
		put(4, inst)
		put(6, menu)
		put(8, parent)
		put(10, uint16(ch))
		put(12, uint16(cw))
		put(14, uint16(y))
		put(16, uint16(x))
		put(18, uint16(style))
		put(20, uint16(style>>16))
		put(22, nameOff)
		put(24, nameSel)
		put(26, clsOff)
		put(28, clsSel)
		lp := uint32(cs.Sel)<<16 | 0

		if r, err := p.SendMessage(w.Handle, WMNCCreate, 0, lp); err != nil {
			return 0, err
		} else if r == 0 {
			p.note("WM_NCCREATE 回 0，視窗 %q 建立失敗", w.Text)
		}
		if r, err := p.SendMessage(w.Handle, WMCreate, 0, lp); err != nil {
			return 0, err
		} else if int32(r) == -1 {
			delete(p.Windows, w.Handle)
			return 0, nil
		}
		p.Mod.Mem.Free(cs.Sel)

		if style&WSVisible != 0 {
			if err := p.showWindow(w, 5); err != nil {
				return 0, err
			}
		}
		return uint32(w.Handle), nil
	}

	h["USER.#42"] = func(p *Process, a Args) (uint32, error) { // ShowWindow
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		was := w.Visible
		if err := p.showWindow(w, a.Word(2)); err != nil {
			return 0, err
		}
		return boolTo(was), nil
	}

	h["USER.#124"] = func(p *Process, a Args) (uint32, error) { // UpdateWindow
		w, ok := p.Window(a.Word(0))
		if !ok || !w.NeedPaint || !w.Visible {
			return 0, nil
		}
		_, err := p.SendMessage(w.Handle, WMPaint, 0, 0)
		return 0, err
	}

	h["USER.#53"] = func(p *Process, a Args) (uint32, error) { // DestroyWindow
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		if _, err := p.SendMessage(w.Handle, WMDestroy, 0, 0); err != nil {
			return 0, err
		}
		delete(p.Windows, w.Handle)
		for i, h := range p.WindowOrder {
			if h == w.Handle {
				p.WindowOrder = append(p.WindowOrder[:i], p.WindowOrder[i+1:]...)
				break
			}
		}
		return 1, nil
	}

	h["USER.#107"] = func(p *Process, a Args) (uint32, error) { // DefWindowProc
		return p.defWindowProc(a.Word(0), a.Word(2), a.Word(4), a.Long(6))
	}
	h["USER.#308"] = func(p *Process, a Args) (uint32, error) { // DefDlgProc
		return p.defWindowProc(a.Word(0), a.Word(2), a.Word(4), a.Long(6))
	}

	h["USER.#111"] = func(p *Process, a Args) (uint32, error) { // SendMessage
		return p.SendMessage(a.Word(0), a.Word(2), a.Word(4), a.Long(6))
	}

	h["USER.#6"] = func(p *Process, a Args) (uint32, error) { // PostQuitMessage
		p.Quit, p.QuitCode = true, a.Word(0)
		return 0, nil
	}

	// PeekMessage(MSG far*, HWND, UINT min, UINT max, UINT flags)
	// flags bit0 ＝ PM_REMOVE。
	h["USER.#109"] = func(p *Process, a Args) (uint32, error) {
		msgSel, msgOff := a.Ptr(0)
		remove := a.Word(10)&1 != 0
		if p.Quit {
			p.writeMsg(msgSel, msgOff, Msg{Message: WMQuit, WParam: p.QuitCode, Time: p.Clock.Millis()})
			return 1, nil
		}
		m, ok := p.nextMessage()
		if !ok {
			return 0, nil
		}
		if !remove {
			// 沒有 PM_REMOVE 就把它放回去。佇列訊息才需要放回；
			// 計時器與重畫本來就是「還在」的狀態，不必還原。
			if m.Message != WMTimer && m.Message != WMPaint {
				p.Queue = append([]Msg{m}, p.Queue...)
			}
		}
		p.writeMsg(msgSel, msgOff, m)
		return 1, nil
	}

	h["USER.#113"] = func(p *Process, _ Args) (uint32, error) { return 0, nil } // TranslateMessage

	h["USER.#114"] = func(p *Process, a Args) (uint32, error) { // DispatchMessage
		sel, off := a.Ptr(0)
		u16 := func(d uint16) uint16 { v, _ := p.Mod.Mem.ReadU16(sel, off+d); return v }
		hwnd, msg, wp := u16(0), u16(2), u16(4)
		lp := uint32(u16(8))<<16 | uint32(u16(6))
		if msg == WMTimer {
			if t := p.findTimer(hwnd, wp); t != nil && (t.ProcSel != 0 || t.ProcOff != 0) {
				return p.Call16(t.ProcSel, t.ProcOff, hwnd, msg, wp, uint16(lp>>16), uint16(lp))
			}
		}
		return p.SendMessage(hwnd, msg, wp, lp)
	}

	// SetTimer(HWND, int id, UINT elapse, TIMERPROC)
	h["USER.#10"] = func(p *Process, a Args) (uint32, error) {
		hwnd, id, elapse := a.Word(0), a.Word(2), a.Word(4)
		procSel, procOff := a.Ptr(6)
		if hwnd == 0 && id == 0 {
			id = p.nextTimerID
			p.nextTimerID++
		}
		if t := p.findTimer(hwnd, id); t != nil {
			t.Elapse, t.ProcSel, t.ProcOff = uint32(elapse), procSel, procOff
			t.NextDue = p.Clock.Millis() + uint32(elapse)
			return uint32(id), nil
		}
		p.Timers = append(p.Timers, Timer{
			HWnd: hwnd, ID: id, Elapse: uint32(elapse),
			ProcSel: procSel, ProcOff: procOff,
			NextDue: p.Clock.Millis() + uint32(elapse),
		})
		return uint32(id), nil
	}

	h["USER.#12"] = func(p *Process, a Args) (uint32, error) { // KillTimer
		hwnd, id := a.Word(0), a.Word(2)
		for i := range p.Timers {
			if p.Timers[i].HWnd == hwnd && p.Timers[i].ID == id {
				p.Timers = append(p.Timers[:i], p.Timers[i+1:]...)
				return 1, nil
			}
		}
		return 0, nil
	}

	// --- DC ---

	h["USER.#66"] = func(p *Process, a Args) (uint32, error) { // GetDC
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		return uint32(p.newWindowDC(w, nil)), nil
	}

	h["USER.#68"] = func(p *Process, a Args) (uint32, error) { // ReleaseDC
		p.Objects.Delete(a.Word(2))
		return 1, nil
	}

	// BeginPaint(HWND, PAINTSTRUCT far*)
	//
	// PAINTSTRUCT：+0 hdc(2) +2 fErase(2) +4 rcPaint(8) +12 fRestore(2)
	//              +14 fIncUpdate(2) +16 rgbReserved(16)
	h["USER.#39"] = func(p *Process, a Args) (uint32, error) {
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		inv := [4]int{w.InvL, w.InvT, w.InvR, w.InvB}
		if !w.NeedPaint {
			inv = [4]int{0, 0, w.ClientW, w.ClientH}
		}
		hdc := p.newWindowDC(w, &inv)
		erase := w.EraseBkg
		if erase {
			if _, err := p.SendMessage(w.Handle, WMEraseBkgnd, hdc, 0); err != nil {
				return 0, err
			}
		}
		sel, off := a.Ptr(2)
		put := func(d uint16, v uint16) { _ = p.Mod.Mem.WriteU16(sel, off+d, v) }
		put(0, hdc)
		put(2, boolWord(erase))
		put(4, uint16(inv[0]))
		put(6, uint16(inv[1]))
		put(8, uint16(inv[2]))
		put(10, uint16(inv[3]))
		put(12, 0)
		put(14, 0)
		p.Validate(w, nil)
		return uint32(hdc), nil
	}

	h["USER.#40"] = func(p *Process, a Args) (uint32, error) { // EndPaint
		sel, off := a.Ptr(2)
		hdc, _ := p.Mod.Mem.ReadU16(sel, off)
		p.Objects.Delete(hdc)
		return 1, nil
	}

	h["USER.#125"] = func(p *Process, a Args) (uint32, error) { // InvalidateRect
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		rSel, rOff := a.Ptr(2)
		var rect *[4]int
		if rSel != 0 {
			r := p.readRect(rSel, rOff)
			rect = &r
		}
		p.Invalidate(w, rect, a.Word(6) != 0)
		return 1, nil
	}

	h["USER.#127"] = func(p *Process, a Args) (uint32, error) { // ValidateRect
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		rSel, rOff := a.Ptr(2)
		var rect *[4]int
		if rSel != 0 {
			r := p.readRect(rSel, rOff)
			rect = &r
		}
		p.Validate(w, rect)
		return 1, nil
	}

	h["USER.#33"] = func(p *Process, a Args) (uint32, error) { // GetClientRect
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		sel, off := a.Ptr(2)
		p.writeRect(sel, off, [4]int{0, 0, w.ClientW, w.ClientH})
		return 1, nil
	}

	h["USER.#32"] = func(p *Process, a Args) (uint32, error) { // GetWindowRect
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		sel, off := a.Ptr(2)
		p.writeRect(sel, off, [4]int{w.X, w.Y, w.X + w.W, w.Y + w.H})
		return 1, nil
	}

	h["USER.#29"] = func(p *Process, a Args) (uint32, error) { // ScreenToClient
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		sel, off := a.Ptr(2)
		x, _ := p.Mod.Mem.ReadU16(sel, off)
		y, _ := p.Mod.Mem.ReadU16(sel, off+2)
		_ = p.Mod.Mem.WriteU16(sel, off, uint16(int16(x)-int16(w.ClientX)))
		_ = p.Mod.Mem.WriteU16(sel, off+2, uint16(int16(y)-int16(w.ClientY)))
		return 1, nil
	}

	h["USER.#56"] = func(p *Process, a Args) (uint32, error) { // MoveWindow
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		w.X, w.Y = int(int16(a.Word(2))), int(int16(a.Word(4)))
		w.W, w.H = int(int16(a.Word(6))), int(int16(a.Word(8)))
		p.layout(w)
		if a.Word(10) != 0 {
			p.Invalidate(w, nil, true)
		}
		return 1, nil
	}

	// --- 矩形工具（純計算，不碰畫面）---

	h["USER.#72"] = func(p *Process, a Args) (uint32, error) { // SetRect
		sel, off := a.Ptr(0)
		p.writeRect(sel, off, [4]int{
			int(int16(a.Word(4))), int(int16(a.Word(6))),
			int(int16(a.Word(8))), int(int16(a.Word(10)))})
		return 1, nil
	}
	h["USER.#77"] = func(p *Process, a Args) (uint32, error) { // OffsetRect
		sel, off := a.Ptr(0)
		r := p.readRect(sel, off)
		dx, dy := int(int16(a.Word(4))), int(int16(a.Word(6)))
		p.writeRect(sel, off, [4]int{r[0] + dx, r[1] + dy, r[2] + dx, r[3] + dy})
		return 1, nil
	}
	h["USER.#78"] = func(p *Process, a Args) (uint32, error) { // InflateRect
		sel, off := a.Ptr(0)
		r := p.readRect(sel, off)
		dx, dy := int(int16(a.Word(4))), int(int16(a.Word(6)))
		p.writeRect(sel, off, [4]int{r[0] - dx, r[1] - dy, r[2] + dx, r[3] + dy})
		return 1, nil
	}
	h["USER.#76"] = func(p *Process, a Args) (uint32, error) { // PtInRect(rect, POINT)
		sel, off := a.Ptr(0)
		r := p.readRect(sel, off)
		x, y := int(int16(a.Word(4))), int(int16(a.Word(6)))
		return boolTo(x >= r[0] && x < r[2] && y >= r[1] && y < r[3]), nil
	}

	// --- 其餘的小東西 ---

	h["USER.#173"] = func(p *Process, _ Args) (uint32, error) { return uint32(p.stockCursor()), nil }
	h["USER.#174"] = func(p *Process, _ Args) (uint32, error) { return uint32(p.stockIcon()), nil }
	h["USER.#69"] = func(p *Process, a Args) (uint32, error) { return uint32(a.Word(0)), nil } // SetCursor
	h["USER.#18"] = func(p *Process, a Args) (uint32, error) { p.Capture = a.Word(0); return 0, nil }
	h["USER.#19"] = func(p *Process, _ Args) (uint32, error) { p.Capture = 0; return 0, nil }
	h["USER.#22"] = func(p *Process, a Args) (uint32, error) { // SetFocus
		old := p.Focus
		p.Focus = a.Word(0)
		return uint32(old), nil
	}
	h["USER.#46"] = func(p *Process, a Args) (uint32, error) { // GetParent
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		return uint32(w.Parent), nil
	}
	h["USER.#48"] = func(p *Process, a Args) (uint32, error) { // IsChild
		w, ok := p.Window(a.Word(2))
		return boolTo(ok && w.Parent == a.Word(0)), nil
	}
	h["USER.#34"] = func(p *Process, a Args) (uint32, error) { // EnableWindow
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		was := w.Enabled
		w.Enabled = a.Word(2) != 0
		return boolTo(!was), nil
	}
	h["USER.#35"] = func(p *Process, a Args) (uint32, error) { // IsWindowEnabled
		w, ok := p.Window(a.Word(0))
		return boolTo(ok && w.Enabled), nil
	}
	h["USER.#37"] = func(p *Process, a Args) (uint32, error) { // SetWindowText
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		sel, off := a.Ptr(2)
		w.Text = p.CString(sel, off)
		return 1, nil
	}
	h["USER.#36"] = func(p *Process, a Args) (uint32, error) { // GetWindowText
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		sel, off := a.Ptr(2)
		max := int(a.Word(6))
		n := 0
		for ; n < len(w.Text) && n < max-1; n++ {
			_ = p.Mod.Mem.WriteU8(sel, off+uint16(n), w.Text[n])
		}
		_ = p.Mod.Mem.WriteU8(sel, off+uint16(n), 0)
		return uint32(n), nil
	}
	h["USER.#133"] = func(p *Process, a Args) (uint32, error) { return p.windowWord(a.Word(0), int(int16(a.Word(2))), nil) }
	h["USER.#134"] = func(p *Process, a Args) (uint32, error) {
		v := a.Word(4)
		return p.windowWord(a.Word(0), int(int16(a.Word(2))), &v)
	}
	h["USER.#136"] = func(p *Process, a Args) (uint32, error) { // SetWindowLong
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		idx := int(int16(a.Word(2)))
		v := a.Long(4)
		switch idx {
		case -4: // GWL_WNDPROC
			old := uint32(w.ProcSel)<<16 | uint32(w.ProcOff)
			w.ProcSel, w.ProcOff = uint16(v>>16), uint16(v)
			return old, nil
		case -16: // GWL_STYLE
			old := w.Style
			w.Style = v
			p.layout(w)
			return old, nil
		}
		if idx >= 0 && idx+4 <= len(w.Extra) {
			old := uint32(w.Extra[idx]) | uint32(w.Extra[idx+1])<<8 |
				uint32(w.Extra[idx+2])<<16 | uint32(w.Extra[idx+3])<<24
			w.Extra[idx] = byte(v)
			w.Extra[idx+1] = byte(v >> 8)
			w.Extra[idx+2] = byte(v >> 16)
			w.Extra[idx+3] = byte(v >> 24)
			return old, nil
		}
		p.note("SetWindowLong 索引 %d 超出 cbWndExtra（%d）", idx, len(w.Extra))
		return 0, nil
	}
	h["USER.#286"] = func(p *Process, _ Args) (uint32, error) { return 0, nil } // GetDesktopWindow
	h["USER.#243"] = func(p *Process, _ Args) (uint32, error) {
		// GetDialogBaseUnits：系統字型的平均字寬與字高。Windows 3.1 在
		// VGA 上是 8×16。**假說**，會影響對話框版面。
		return 16<<16 | 8, nil
	}
	h["USER.#17"] = func(p *Process, a Args) (uint32, error) { // GetCursorPos
		sel, off := a.Ptr(0)
		_ = p.Mod.Mem.WriteU16(sel, off, uint16(p.CursorX))
		_ = p.Mod.Mem.WriteU16(sel, off+2, uint16(p.CursorY))
		return 0, nil
	}
	h["USER.#222"] = func(p *Process, a Args) (uint32, error) { // GetKeyboardState
		sel, off := a.Ptr(0)
		for i := 0; i < 256; i++ {
			_ = p.Mod.Mem.WriteU8(sel, off+uint16(i), 0)
		}
		return 0, nil
	}
	h["USER.#223"] = func(p *Process, _ Args) (uint32, error) { return 0, nil } // SetKeyboardState
}

// showWindow 是 ShowWindow 與 CreateWindow(WS_VISIBLE) 的共同路徑。
func (p *Process) showWindow(w *Window, cmd uint16) error {
	visible := cmd != 0 // SW_HIDE ＝ 0
	if w.Visible == visible {
		if visible {
			p.Invalidate(w, nil, true)
		}
		return nil
	}
	w.Visible = visible
	if _, err := p.SendMessage(w.Handle, WMShowWindow, boolWord(visible), 0); err != nil {
		return err
	}
	if visible {
		p.Invalidate(w, nil, true)
	}
	return nil
}

// defWindowProc 是 DefWindowProc／DefDlgProc 的共同實作。
func (p *Process) defWindowProc(hwnd, msg, wParam uint16, lParam uint32) (uint32, error) {
	w, ok := p.Window(hwnd)
	if !ok {
		return 0, nil
	}
	switch msg {
	case WMNCCreate:
		return 1, nil
	case WMEraseBkgnd:
		dc, ok := p.dc(wParam)
		if !ok || w.Class.Background == 0 {
			return 1, nil
		}
		if obj, ok := p.Objects.Get(w.Class.Background, ObjBrush); ok && !obj.Brush.Hollow {
			dc.FillRect(0, 0, w.ClientW, w.ClientH, obj.Brush.Index)
		}
		return 1, nil
	case WMPaint:
		// 沒有人畫就當作畫過了，否則 WM_PAINT 會一直重來。
		p.Validate(w, nil)
		return 0, nil
	case WMClose:
		if _, err := p.SendMessage(hwnd, WMDestroy, 0, 0); err != nil {
			return 0, err
		}
		delete(p.Windows, hwnd)
		return 0, nil
	}
	return 0, nil
}

// newWindowDC 造一個畫在螢幕上、原點在客戶區左上角的 DC。
func (p *Process) newWindowDC(w *Window, clip *[4]int) uint16 {
	d := &DC{
		Surf: p.Screen, Window: w.Handle,
		OrgX: w.ClientX, OrgY: w.ClientY,
		ClipL: w.ClientX, ClipT: w.ClientY,
		ClipR: w.ClientX + w.ClientW, ClipB: w.ClientY + w.ClientH,
		BkMode: 2, // OPAQUE
	}
	if clip != nil {
		d.ClipL = max(d.ClipL, w.ClientX+clip[0])
		d.ClipT = max(d.ClipT, w.ClientY+clip[1])
		d.ClipR = min(d.ClipR, w.ClientX+clip[2])
		d.ClipB = min(d.ClipB, w.ClientY+clip[3])
	}
	d.ClipR = min(d.ClipR, p.Screen.W)
	d.ClipB = min(d.ClipB, p.Screen.H)
	h := p.Objects.Add(&Object{Kind: ObjDC, DC: d})
	d.Handle = h
	return h
}

// dc 取一個 DC。
func (p *Process) dc(h uint16) (*DC, bool) {
	obj, ok := p.Objects.Get(h, ObjDC)
	if !ok {
		return nil, false
	}
	return obj.DC, true
}

func (p *Process) findTimer(hwnd, id uint16) *Timer {
	for i := range p.Timers {
		if p.Timers[i].HWnd == hwnd && p.Timers[i].ID == id {
			return &p.Timers[i]
		}
	}
	return nil
}

func (p *Process) readRect(sel, off uint16) [4]int {
	var r [4]int
	for i := 0; i < 4; i++ {
		v, _ := p.Mod.Mem.ReadU16(sel, off+uint16(i*2))
		r[i] = int(int16(v))
	}
	return r
}

func (p *Process) writeRect(sel, off uint16, r [4]int) {
	for i := 0; i < 4; i++ {
		_ = p.Mod.Mem.WriteU16(sel, off+uint16(i*2), uint16(int16(r[i])))
	}
}

func (p *Process) writeMsg(sel, off uint16, m Msg) {
	put := func(d uint16, v uint16) { _ = p.Mod.Mem.WriteU16(sel, off+d, v) }
	put(0, m.HWnd)
	put(2, m.Message)
	put(4, m.WParam)
	put(6, uint16(m.LParam))
	put(8, uint16(m.LParam>>16))
	put(10, uint16(m.Time))
	put(12, uint16(m.Time>>16))
	put(14, uint16(m.PtX))
	put(16, uint16(m.PtY))
}

func (p *Process) windowWord(hwnd uint16, idx int, set *uint16) (uint32, error) {
	w, ok := p.Window(hwnd)
	if !ok {
		return 0, nil
	}
	switch idx {
	case -6: // GWW_HINSTANCE
		return uint32(w.Instance), nil
	case -8: // GWW_HWNDPARENT
		return uint32(w.Parent), nil
	case -12: // GWW_ID
		return uint32(w.Menu), nil
	}
	if idx < 0 || idx+2 > len(w.Extra) {
		p.note("GetWindowWord/SetWindowWord 索引 %d 超出 cbWndExtra（%d）", idx, len(w.Extra))
		return 0, nil
	}
	old := uint32(w.Extra[idx]) | uint32(w.Extra[idx+1])<<8
	if set != nil {
		w.Extra[idx] = byte(*set)
		w.Extra[idx+1] = byte(*set >> 8)
	}
	return old, nil
}

func (p *Process) stockCursor() uint16 {
	if p.cursorHandle == 0 {
		p.cursorHandle = p.Objects.Add(&Object{Kind: ObjBitmap, Bitmap: &Bitmap{Surf: NewSurface(32, 32), Planes: 1, BPP: 1}, Stock: true})
	}
	return p.cursorHandle
}

func (p *Process) stockIcon() uint16 {
	if p.iconHandle == 0 {
		p.iconHandle = p.Objects.Add(&Object{Kind: ObjBitmap, Bitmap: &Bitmap{Surf: NewSurface(32, 32), Planes: 1, BPP: 1}, Stock: true})
	}
	return p.iconHandle
}

func boolTo(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

func boolWord(b bool) uint16 {
	if b {
		return 1
	}
	return 0
}

func upper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 32
		}
	}
	return string(b)
}

var _ = fmt.Sprintf

// RegisterUserDraw 登記 USER 這一側會畫東西的那幾支（矩形填色、
// 調色盤選取），以及還沒接字型的 DrawText。
func RegisterUserDraw(p *Process) {
	h := p.Handlers

	h["USER.#81"] = func(p *Process, a Args) (uint32, error) { // FillRect(hdc, rect, brush)
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		sel, off := a.Ptr(2)
		r := p.readRect(sel, off)
		obj, ok := p.Objects.Get(a.Word(6), ObjBrush)
		if !ok || obj.Brush.Hollow {
			return 1, nil
		}
		d.FillRect(r[0], r[1], r[2]-r[0], r[3]-r[1], obj.Brush.Index)
		return 1, nil
	}

	// FrameRect 畫一圈**一像素寬**的框，右下不含——civ1 那邊實測
	// 30×30 的框是 116 個像素（30×4−4），正是這個語意。
	h["USER.#83"] = func(p *Process, a Args) (uint32, error) {
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		sel, off := a.Ptr(2)
		r := p.readRect(sel, off)
		obj, ok := p.Objects.Get(a.Word(6), ObjBrush)
		if !ok || obj.Brush.Hollow {
			return 1, nil
		}
		v := obj.Brush.Index
		w, hgt := r[2]-r[0], r[3]-r[1]
		if w <= 0 || hgt <= 0 {
			return 1, nil
		}
		d.FillRect(r[0], r[1], w, 1, v)
		d.FillRect(r[0], r[3]-1, w, 1, v)
		d.FillRect(r[0], r[1], 1, hgt, v)
		d.FillRect(r[2]-1, r[1], 1, hgt, v)
		return 1, nil
	}

	h["USER.#85"] = func(p *Process, _ Args) (uint32, error) { // DrawText
		p.note("DrawText 沒有畫字（還沒接原版點陣字型）")
		return 0, nil
	}

	h["USER.#282"] = func(p *Process, a Args) (uint32, error) { // SelectPalette
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		old := d.Pal
		d.Pal = a.Word(2)
		return uint32(old), nil
	}

	h["USER.#283"] = func(p *Process, a Args) (uint32, error) { // RealizePalette
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		obj, ok := p.Objects.Get(d.Pal, ObjPalette)
		if !ok {
			return 0, nil
		}
		return uint32(p.realizePalette(obj.Palette)), nil
	}
}

// RegisterAll 登記目前實作好的全部處理器。
func RegisterAll(p *Process) {
	RegisterKernel(p)
	RegisterWin87EM(p)
	RegisterUser(p)
	RegisterUserWindow(p)
	RegisterUserDraw(p)
	RegisterGDI(p)
	RegisterFile(p)
	RegisterResource(p)
}
