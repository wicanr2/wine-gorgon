package win16

import "fmt"

// RegisterUser 登記 USER 的處理器。
func RegisterUser(p *Process) {
	h := p.Handlers

	// InitApp(HINSTANCE)：真 Windows 在這裡建訊息佇列。回 0 會讓 Borland
	// 的啟動碼直接跳到 INT 21h AH=4Ch 結束（`000F:0040` 的 `or ax,ax`）。
	h["USER.#5"] = func(p *Process, _ Args) (uint32, error) { return 1, nil }

	// GetFreeSystemResources(UINT)：回百分比。CIV.EXE 在
	// `01F7:0020` 一連問三次（系統／GDI／USER），任何一個低於 0x23
	// 就跳出「資源不足」的訊息框並結束。
	h["USER.#284"] = func(p *Process, _ Args) (uint32, error) { return 100, nil }

	// MessageBox：這一層沒有畫面，但**訊息框是最好的診斷輸出**——
	// 遊戲用它報「資源不足」「找不到檔案」。記下來，別吞掉。
	h["USER.#1"] = func(p *Process, a Args) (uint32, error) {
		textSel, textOff := a.Ptr(2)
		capSel, capOff := a.Ptr(6)
		box := MessageBoxCall{
			Text:    p.CString(textSel, textOff),
			Caption: p.CString(capSel, capOff),
			Style:   a.Word(10),
			Steps:   p.CPU.Steps,
		}
		p.MessageBoxes = append(p.MessageBoxes, box)
		return 1, nil // IDOK
	}

	h["USER.#13"] = func(p *Process, _ Args) (uint32, error) { return p.Clock.Millis(), nil } // GetTickCount
	h["USER.#104"] = func(p *Process, _ Args) (uint32, error) { return 0, nil }               // MessageBeep

	// GetSystemMetrics 查 p.Metrics。表上沒有的回 0 並記進 Notes——
	// 「回 0 也看起來正常」是最難查的那種錯。
	h["USER.#179"] = func(p *Process, a Args) (uint32, error) {
		i := int(int16(a.Word(0)))
		if v, ok := p.Metrics[i]; ok {
			return uint32(v), nil
		}
		p.note("GetSystemMetrics(%d) 回 0（表上沒有這一項）", i)
		return 0, nil
	}
}

// MessageBoxCall 是一次 MessageBox 呼叫的紀錄。
type MessageBoxCall struct {
	Text    string
	Caption string
	Style   uint16
	Steps   uint64
}

// CString 讀一個以零結尾的字串（最長 1024，防止讀到天荒地老）。
func (p *Process) CString(sel, off uint16) string {
	var b []byte
	for i := 0; i < 1024; i++ {
		v, err := p.Mod.Mem.ReadU8(sel, off+uint16(i))
		if err != nil || v == 0 {
			break
		}
		b = append(b, v)
	}
	return string(b)
}

// note 記一筆「做了但不完整」的事。這些不是錯誤，是**下一輪的工作清單**；
// 靜靜回 0 才是問題。
func (p *Process) note(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	if p.seenNotes == nil {
		p.seenNotes = map[string]bool{}
	}
	if p.seenNotes[msg] {
		return
	}
	p.seenNotes[msg] = true
	p.Notes = append(p.Notes, msg)
}
