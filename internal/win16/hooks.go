package win16

// Win16 的 hook 型別編號。只列出有實際派送的；其餘照樣登記得起來，
// 但會在登記時說明它不會被呼叫——安靜地不呼叫會讓遊戲看起來只是「沒反應」。
const (
	WHMsgFilter  = -1
	WHKeyboard   = 2
	WHGetMessage = 3
	WHCallWndMsg = 4
)

// Hook 是一個裝上去的訊息掛鉤。
type Hook struct {
	Handle uint16
	ID     int
	Sel    uint16
	Off    uint16
	Task   uint16
}

// SetHook 登記一個 hook，回傳它的代號。
func (p *Process) SetHook(id int, sel, off, task uint16) uint16 {
	p.hookNext++
	h := &Hook{Handle: p.hookNext, ID: id, Sel: sel, Off: off, Task: task}
	p.Hooks = append(p.Hooks, h)
	return h.Handle
}

// RemoveHook 拆掉一個 hook。回傳有沒有找到。
func (p *Process) RemoveHook(handle uint16) bool {
	for i, h := range p.Hooks {
		if h.Handle != handle {
			continue
		}
		p.Hooks = append(p.Hooks[:i], p.Hooks[i+1:]...)
		return true
	}
	return false
}

// callGetMessageHook 把一則剛取出來的訊息交給 WH_GETMESSAGE 的掛鉤。
//
// 這一步不能省。PTO2 播開場影片時就是靠它攔按鍵與滑鼠
// （`RE:cseg11:0x1f6a`：`SetWindowsHookEx(3, …)`）；不呼叫的話影片放完之前
// 什麼輸入都不會被看到，而畫面完全正常——只是不動。
//
// 掛鉤程序是 far pascal (int nCode, WORD wParam, LONG lParam)，
// lParam 是那則 MSG 的 far pointer；nCode = HC_ACTION = 0。
func (p *Process) callGetMessageHook(msgSel, msgOff, remove uint16) error {
	for _, h := range p.Hooks {
		if h.ID != WHGetMessage {
			continue
		}
		// far 指標要先推段再推位移：pascal 由左往右推，而後推的落在低位址，
		// 所以 `les bx,[bp+6]` 讀到的低位字必須是位移。
		if _, err := p.Call16(h.Sel, h.Off,
			0,      // nCode = HC_ACTION
			remove, // wParam：PM_REMOVE 與否
			msgSel, msgOff); err != nil {
			return err
		}
	}
	return nil
}
