package win16

// 滑鼠與鍵盤訊息。
const (
	WMMouseMove   = 0x0200
	WMLButtonDown = 0x0201
	WMLButtonUp   = 0x0202
	WMLButtonDbl  = 0x0203
	WMRButtonDown = 0x0204
	WMRButtonUp   = 0x0205
	WMKeyDown     = 0x0100
	WMKeyUp       = 0x0101
	WMChar        = 0x0102
	WMCommand     = 0x0111
)

// WindowAt 找螢幕座標 (x,y) 上最上面的可見視窗。
//
// 順序是**建立順序的反向**：後建的在上面。真 Windows 的 Z 序是可以改的
// （BringWindowToTop 一族），CIV.EXE 沒有匯入那些，所以這個近似對它成立。
func (p *Process) WindowAt(x, y int) (*Window, bool) {
	for i := len(p.WindowOrder) - 1; i >= 0; i-- {
		w, ok := p.Windows[p.WindowOrder[i]]
		if !ok || !w.Visible || w.Handle == p.Desktop {
			continue
		}
		if x >= w.AbsX && x < w.AbsX+w.W && y >= w.AbsY && y < w.AbsY+w.H {
			return w, true
		}
	}
	if w, ok := p.Windows[p.Desktop]; ok {
		return w, true
	}
	return nil, false
}

// PostMouse 把一則滑鼠訊息送給 (x,y) 上的視窗。
//
// 座標換成**客戶座標**放進 lParam——視窗程序讀到的就是這個，
// 所以命中判定必須和畫面用的是同一套版面計算。
func (p *Process) PostMouse(msg uint16, x, y int, keys uint16) uint16 {
	p.CursorX, p.CursorY = x, y
	target := p.Capture
	if target == 0 {
		w, ok := p.WindowAt(x, y)
		if !ok {
			return 0
		}
		target = w.Handle
	}
	w := p.Windows[target]
	cx, cy := x-w.ClientX, y-w.ClientY
	p.Queue = append(p.Queue, Msg{
		HWnd: target, Message: msg, WParam: keys,
		LParam: uint32(uint16(int16(cy)))<<16 | uint32(uint16(int16(cx))),
		Time:   p.Clock.Millis(), PtX: int16(x), PtY: int16(y),
	})
	return target
}

// PostKey 把一則鍵盤訊息送給焦點視窗（沒有焦點就送給最上面那個）。
func (p *Process) PostKey(msg uint16, vk uint16) uint16 {
	target := p.Focus
	if target == 0 {
		for i := len(p.WindowOrder) - 1; i >= 0; i-- {
			w, ok := p.Windows[p.WindowOrder[i]]
			if ok && w.Visible && w.Handle != p.Desktop {
				target = w.Handle
				break
			}
		}
	}
	if target == 0 {
		return 0
	}
	p.Queue = append(p.Queue, Msg{
		HWnd: target, Message: msg, WParam: vk,
		LParam: 1, Time: p.Clock.Millis(),
	})
	return target
}

// Click 送一組「按下再放開」。
func (p *Process) Click(x, y int) uint16 {
	p.PostMouse(WMMouseMove, x, y, 0)
	h := p.PostMouse(WMLButtonDown, x, y, 1)
	p.PostMouse(WMLButtonUp, x, y, 0)
	return h
}

// TypeKey 送一組「按下、字元、放開」。
func (p *Process) TypeKey(vk uint16) {
	p.PostKey(WMKeyDown, vk)
	if vk >= 0x20 && vk < 0x7F || vk == 13 || vk == 27 || vk == 8 {
		p.PostKey(WMChar, vk)
	}
	p.PostKey(WMKeyUp, vk)
}
