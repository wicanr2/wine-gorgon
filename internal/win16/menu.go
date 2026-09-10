package win16

// 選單。CIV.EXE 不呼叫 LoadMenu／SetMenu／TrackPopupMenu——它的選單由視窗
// 類別的 `lpszMenuName` 帶進來，由載入器隱式載入。程式只呼叫
// `GetMenu`、`CheckMenuItem`、`EnableMenuItem`、`ModifyMenu`（實跑一趟分別是
// 1／24／81／16 次），也就是說**它只讀寫選單項的狀態，不自己組選單**。
//
// 所以這裡做兩件事：把 RT_MENU 範本解析成一棵樹，並把勾選／啟用狀態記住。
// 「點一個選單項」則是把 `WM_COMMAND` 送給擁有選單的視窗——真 Windows 的
// 選單追蹤（滑鼠反白、彈出視窗、鍵盤導覽）不在這一層，對拍腳本要的是
// **那個命令有沒有進到程式的 WndProc**，不是選單長什麼樣。

import (
	"fmt"
	"strings"

	"github.com/wicanr2/wine-gorgon/internal/ne"
)

// 選單項旗標（Win16 的 MF_*）。
const (
	MFGrayed    = 0x0001
	MFDisabled  = 0x0002
	MFChecked   = 0x0008
	MFPopup     = 0x0010
	MFSeparator = 0x0800
	MFEnd       = 0x0080

	// MFByCommand／MFByPosition 是 CheckMenuItem 一族的第三個參數。
	MFByCommand  = 0x0000
	MFByPosition = 0x0400
)

// MenuItem 是一個選單項；`Children` 非空表示它是彈出子選單。
type MenuItem struct {
	Text     string
	ID       uint16
	Flags    uint16
	Children []*MenuItem
}

// IsPopup 回報這一項是不是子選單。**用結構判斷不用旗標**：範本裡的
// MF_POPUP 位元與「有沒有子項」理論上一致，但解析錯位時前者會說謊。
func (m *MenuItem) IsPopup() bool { return len(m.Children) > 0 }

// Checked／Disabled 是目前狀態；程式用 CheckMenuItem／EnableMenuItem 改。
func (m *MenuItem) Checked() bool  { return m.Flags&MFChecked != 0 }
func (m *MenuItem) Disabled() bool { return m.Flags&(MFGrayed|MFDisabled) != 0 }

// Menu 是一份載好的選單。
type Menu struct {
	Handle uint16
	Name   string
	Items  []*MenuItem
}

// Find 依命令 id 找項目（含子選單）。
func (m *Menu) Find(id uint16) *MenuItem {
	var walk func(items []*MenuItem) *MenuItem
	walk = func(items []*MenuItem) *MenuItem {
		for _, it := range items {
			if !it.IsPopup() && it.ID == id {
				return it
			}
			if hit := walk(it.Children); hit != nil {
				return hit
			}
		}
		return nil
	}
	return walk(m.Items)
}

// FindText 依顯示文字找項目。比對時**忽略 `&` 助憶符與 Tab 之後的快捷鍵
// 說明**——腳本寫 `Animations` 就該對得上範本裡的 `Ani&mations`。
func (m *Menu) FindText(want string) *MenuItem {
	want = strings.ToLower(want)
	var walk func(items []*MenuItem) *MenuItem
	walk = func(items []*MenuItem) *MenuItem {
		for _, it := range items {
			if !it.IsPopup() && strings.Contains(menuLabel(it.Text), want) {
				return it
			}
			if hit := walk(it.Children); hit != nil {
				return hit
			}
		}
		return nil
	}
	return walk(m.Items)
}

func menuLabel(text string) string {
	if i := strings.IndexByte(text, '\t'); i >= 0 {
		text = text[:i]
	}
	return strings.ToLower(strings.ReplaceAll(text, "&", ""))
}

// parseMenu 解析 RT_MENU 範本。
//
// 版面：`version`(2) `headerSize`(2) 之後是項目串流。每一項是
// `flags`(2)、非 POPUP 再接 `id`(2)，然後 NUL 結尾字串。MF_POPUP 表示接
// 下來是一層子項，該層的最後一項帶 MF_END；子層讀完回到上一層。
func parseMenu(data []byte) ([]*MenuItem, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("選單範本只有 %d bytes", len(data))
	}
	version := uint16(data[0]) | uint16(data[1])<<8
	if version != 0 {
		return nil, fmt.Errorf("不認得的選單範本版本 %d", version)
	}
	offset := int(uint16(data[2]) | uint16(data[3])<<8)
	pos := 4 + offset
	items, _, err := parseMenuLevel(data, pos)
	return items, err
}

func parseMenuLevel(data []byte, pos int) ([]*MenuItem, int, error) {
	var items []*MenuItem
	for {
		if pos+2 > len(data) {
			return nil, pos, fmt.Errorf("選單範本在 %d 截斷", pos)
		}
		flags := uint16(data[pos]) | uint16(data[pos+1])<<8
		pos += 2
		item := &MenuItem{Flags: flags &^ MFEnd}
		if flags&MFPopup == 0 {
			if pos+2 > len(data) {
				return nil, pos, fmt.Errorf("選單項在 %d 缺 id", pos)
			}
			item.ID = uint16(data[pos]) | uint16(data[pos+1])<<8
			pos += 2
		}
		start := pos
		for pos < len(data) && data[pos] != 0 {
			pos++
		}
		if pos >= len(data) {
			return nil, pos, fmt.Errorf("選單項的字串在 %d 沒有結尾", start)
		}
		item.Text = string(data[start:pos])
		pos++
		if flags&MFPopup != 0 {
			children, next, err := parseMenuLevel(data, pos)
			if err != nil {
				return nil, next, err
			}
			item.Children, pos = children, next
		}
		items = append(items, item)
		if flags&MFEnd != 0 {
			return items, pos, nil
		}
	}
}

// loadClassMenu 載入視窗類別 `lpszMenuName` 指的那份選單。
//
// 真 Windows 在 CreateWindow 裡做這件事，程式看不到 LoadMenu 的呼叫——
// CIV.EXE 就是這樣：它從頭到尾只用 GetMenu 拿 handle，再對項目下
// CheckMenuItem／EnableMenuItem。**選單解析不了不能讓建視窗失敗**：
// 選單只影響外觀與命令派送，而視窗本身還有別的用途。
func (p *Process) loadClassMenu(name string) uint16 {
	if name == "" {
		return 0
	}
	id, resName := menuResourceName(name)
	r, ok := p.Mod.Image.FindResource(ne.RTMenu, "", resName, id)
	if !ok {
		p.note("選單：找不到 RT_MENU %s", name)
		return 0
	}
	data, err := p.Mod.Image.ResourceData(r)
	if err != nil {
		p.note("選單：讀不到 %s：%v", name, err)
		return 0
	}
	items, err := parseMenu(data)
	if err != nil {
		p.note("選單：%s 解析失敗：%v", name, err)
		return 0
	}
	h := p.nextHMenu
	p.nextHMenu++
	p.Menus[h] = &Menu{Handle: h, Name: name, Items: items}
	p.note("選單 %s → HMENU %04X，%d 個頂層項", name, h, len(items))
	return h
}

// menuResourceName 把 `lpszMenuName` 換成 FindResource 的兩種形式。
// RegisterClass 那一層把 MAKEINTRESOURCE(n) 記成 `#n`。
func menuResourceName(name string) (uint16, string) {
	if strings.HasPrefix(name, "#") {
		var id uint16
		if _, err := fmt.Sscanf(name[1:], "%d", &id); err == nil {
			return id, ""
		}
	}
	return 0, name
}

// MenuOf 回傳某個視窗的選單。
func (p *Process) MenuOf(hwnd uint16) (*Menu, bool) {
	w, ok := p.Window(hwnd)
	if !ok || w.Menu == 0 {
		return nil, false
	}
	m, ok := p.Menus[w.Menu]
	return m, ok
}

// MenuWindow 找出擁有選單的那個視窗（CIV.EXE 只有一個）。
func (p *Process) MenuWindow() (uint16, *Menu, bool) {
	for _, h := range p.WindowOrder {
		if m, ok := p.MenuOf(h); ok {
			return h, m, true
		}
	}
	return 0, nil, false
}

// ClickMenu 選一個選單項：把 `WM_COMMAND` 送給擁有選單的視窗。
//
// wParam ＝ 命令 id、lParam ＝ 0（真 Windows 對選單就是送 0，控制項才會在
// lParam 放 handle 與通知碼）。**停用中的項目不送**——真 Windows 也點不到，
// 而腳本要是點得到，就會量到一條玩家走不到的路徑。
func (p *Process) ClickMenu(item *MenuItem) (uint16, error) {
	hwnd, _, ok := p.MenuWindow()
	if !ok {
		return 0, fmt.Errorf("沒有任何視窗帶選單")
	}
	if item.IsPopup() {
		return 0, fmt.Errorf("%q 是子選單不是命令", item.Text)
	}
	if item.Disabled() {
		return 0, fmt.Errorf("選單項 %q（id %d）目前是停用的", item.Text, item.ID)
	}
	p.Queue = append(p.Queue, Msg{
		HWnd: hwnd, Message: WMCommand, WParam: item.ID,
		LParam: 0, Time: p.Clock.Millis(),
	})
	return hwnd, nil
}

// menuItem 依 `MF_BYCOMMAND`／`MF_BYPOSITION` 找項目。
//
// **兩種定址不能混**：BYPOSITION 的 0 是「第一項」，BYCOMMAND 的 0 是
// 「命令 0」（分隔線就是 id 0）。混掉的話 EnableMenuItem(0, MF_BYPOSITION)
// 會去停用某條分隔線，而分隔線沒有人看得見——錯誤完全不會浮出來。
func (p *Process) menuItem(hmenu, which, flags uint16) (*Menu, *MenuItem) {
	m, ok := p.Menus[hmenu]
	if !ok {
		return nil, nil
	}
	if flags&MFByPosition != 0 {
		if int(which) < len(m.Items) {
			return m, m.Items[which]
		}
		return m, nil
	}
	return m, m.Find(which)
}

// setMenuItemFlags 是 CheckMenuItem／EnableMenuItem 的共同實作：把 `mask`
// 那幾個位元換成 `flags` 裡的值，回傳**舊的**那幾位元（Win16 的契約）。
func (p *Process) setMenuItemFlags(hmenu, which, flags, mask uint16) (uint32, error) {
	_, item := p.menuItem(hmenu, which, flags)
	if item == nil {
		return 0xFFFFFFFF, nil // Win16：找不到回 -1
	}
	prev := item.Flags & mask
	item.Flags = (item.Flags &^ mask) | (flags & mask)
	return uint32(prev), nil
}
