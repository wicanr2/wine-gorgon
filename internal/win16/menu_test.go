package win16

import (
	"testing"

	"github.com/wicanr2/wine-gorgon/internal/cpu"
)

// buildMenu 組一份**合成的** RT_MENU 範本（原版執行檔不進版控）。
// 形狀照 Win16：version(2)、headerSize(2)，之後每一項是 flags(2)、
// 非 POPUP 再接 id(2)，然後 NUL 結尾字串。
//
// 樹長這樣（與 CIV.EXE 的形狀同構：Options 是 File 底下的子選單）：
//
//	&File
//	  &Save\tCtrl+S   101
//	  &Options
//	    &Instant Advice 105
//	    Ani&mations     108   ← 最後一項帶 MF_END
//	  &Quit            115   ← 子選單結束之後還有這一項
//	&Edit                    ← 頂層最後一項
//	  &Undo            201
func buildMenu() []byte {
	var out []byte
	u16 := func(v uint16) { out = append(out, byte(v), byte(v>>8)) }
	str := func(s string) { out = append(out, s...); out = append(out, 0) }
	cmd := func(flags, id uint16, text string) { u16(flags); u16(id); str(text) }
	popup := func(flags uint16, text string) { u16(flags | MFPopup); str(text) }

	u16(0) // version
	u16(0) // headerSize

	popup(0, "&File")
	cmd(0, 101, "&Save\tCtrl+S")
	popup(0, "&Options")
	cmd(0, 105, "&Instant Advice")
	cmd(MFEnd, 108, "Ani&mations")
	cmd(MFEnd, 115, "&Quit")

	popup(MFEnd, "&Edit")
	cmd(MFEnd, 201, "&Undo")
	return out
}

func TestParseMenuBuildsTheTree(t *testing.T) {
	items, err := parseMenu(buildMenu())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("頂層 %d 項，預期 2（File、Edit）", len(items))
	}
	file := items[0]
	if file.Text != "&File" || !file.IsPopup() {
		t.Fatalf("第一項是 %+v", file)
	}
	// File 底下：Save、[Options]、Quit
	if len(file.Children) != 3 {
		t.Fatalf("File 有 %d 個子項，預期 3", len(file.Children))
	}
	opts := file.Children[1]
	if !opts.IsPopup() || opts.Text != "&Options" {
		t.Fatalf("Options 是 %+v", opts)
	}
	if len(opts.Children) != 2 {
		t.Fatalf("Options 有 %d 項，預期 2", len(opts.Children))
	}
	if got := opts.Children[1]; got.Text != "Ani&mations" || got.ID != 108 {
		t.Fatalf("Animations 是 %+v", got)
	}
}

// **子選單的結尾不能把上一層也結束掉**：巢狀那一層的 MF_END 只結束它自己。
// 讀錯的話 Quit 與 Edit 會整個消失，而解析仍然「成功」。
func TestParseMenuNestedEndDoesNotCloseTheParent(t *testing.T) {
	items, err := parseMenu(buildMenu())
	if err != nil {
		t.Fatal(err)
	}
	file := items[0]
	if got := file.Children[2]; got.Text != "&Quit" || got.ID != 115 {
		t.Fatalf("子選單之後的那一項是 %+v，預期 &Quit", got)
	}
	if items[1].Text != "&Edit" {
		t.Fatalf("第二個頂層項是 %q，預期 &Edit", items[1].Text)
	}
}

func TestParseMenuRejectsTruncatedTemplate(t *testing.T) {
	data := buildMenu()
	if _, err := parseMenu(data[:len(data)-3]); err == nil {
		t.Fatal("截斷的範本應該要報錯，不是安靜地少幾項")
	}
	if _, err := parseMenu([]byte{9, 0, 0, 0}); err == nil {
		t.Fatal("不認得的版本應該要報錯")
	}
}

func newMenuProcess(t *testing.T) (*Process, *Menu) {
	t.Helper()
	items, err := parseMenu(buildMenu())
	if err != nil {
		t.Fatal(err)
	}
	p := &Process{Menus: map[uint16]*Menu{}, Windows: map[uint16]*Window{}}
	m := &Menu{Handle: 0x0300, Name: "#128", Items: items}
	p.Menus[m.Handle] = m
	w := &Window{Handle: 0x0800, Menu: m.Handle, HasMenu: true, Visible: true}
	p.Windows[w.Handle] = w
	p.WindowOrder = []uint16{w.Handle}
	return p, m
}

func TestFindTextIgnoresAmpersandAndAccelerator(t *testing.T) {
	_, m := newMenuProcess(t)
	if got := m.FindText("animations"); got == nil || got.ID != 108 {
		t.Fatalf("FindText(animations) → %+v", got)
	}
	if got := m.FindText("save"); got == nil || got.ID != 101 {
		t.Fatalf("FindText(save) → %+v（`\\tCtrl+S` 應該被忽略）", got)
	}
	if got := m.FindText("nonexistent"); got != nil {
		t.Fatalf("不存在的項目回了 %+v", got)
	}
}

func TestCheckMenuItemRemembersTheState(t *testing.T) {
	p, m := newMenuProcess(t)
	item := m.Find(108)
	if item.Checked() {
		t.Fatal("初始不該是勾選的")
	}
	prev, err := p.setMenuItemFlags(m.Handle, 108, MFChecked|MFByCommand, MFChecked)
	if err != nil {
		t.Fatal(err)
	}
	if prev != 0 {
		t.Fatalf("舊狀態回 %d，預期 0（Win16 的契約是回舊值）", prev)
	}
	if !item.Checked() {
		t.Fatal("勾選沒有被記住")
	}
	prev, _ = p.setMenuItemFlags(m.Handle, 108, MFByCommand, MFChecked)
	if prev != MFChecked {
		t.Fatalf("第二次的舊狀態回 %d，預期 MFChecked", prev)
	}
	if item.Checked() {
		t.Fatal("取消勾選沒有被記住")
	}
}

// **BYCOMMAND 與 BYPOSITION 不能混**：位置 0 是第一個頂層項，
// 命令 0 是別的東西。混掉的話停用會落到看不見的地方，錯誤不會浮出來。
func TestMenuItemAddressingModesAreDistinct(t *testing.T) {
	p, m := newMenuProcess(t)
	_, byPos := p.menuItem(m.Handle, 0, MFByPosition)
	if byPos == nil || byPos.Text != "&File" {
		t.Fatalf("BYPOSITION 0 → %+v，預期 &File", byPos)
	}
	if _, byCmd := p.menuItem(m.Handle, 0, MFByCommand); byCmd != nil {
		t.Fatalf("BYCOMMAND 0 不該中任何項目，卻回了 %+v", byCmd)
	}
	if _, missing := p.menuItem(m.Handle, 99, MFByPosition); missing != nil {
		t.Fatal("越界的位置應該回 nil")
	}
	if got, _ := p.setMenuItemFlags(0x9999, 108, MFChecked, MFChecked); got != 0xFFFFFFFF {
		t.Fatalf("不存在的 HMENU 應該回 -1，卻回了 %d", got)
	}
}

func TestClickMenuPostsWMCommand(t *testing.T) {
	p, m := newMenuProcess(t)
	p.Clock = &StepClock{CPU: &cpu.CPU{}}
	hwnd, err := p.ClickMenu(m.Find(108))
	if err != nil {
		t.Fatal(err)
	}
	if hwnd != 0x0800 {
		t.Fatalf("送給視窗 %04X，預期 0800", hwnd)
	}
	if len(p.Queue) != 1 {
		t.Fatalf("佇列裡有 %d 則訊息", len(p.Queue))
	}
	msg := p.Queue[0]
	if msg.Message != WMCommand || msg.WParam != 108 || msg.LParam != 0 {
		t.Fatalf("送出的是 %+v，預期 WM_COMMAND wParam=108 lParam=0", msg)
	}
}

// 停用中的項目點不到——真 Windows 也點不到，腳本點得到就會量到一條
// 玩家走不到的路徑。
func TestClickMenuRefusesDisabledAndPopupItems(t *testing.T) {
	p, m := newMenuProcess(t)
	p.Clock = &StepClock{CPU: &cpu.CPU{}}
	if _, err := p.ClickMenu(m.Items[0]); err == nil {
		t.Fatal("子選單不是命令，應該報錯")
	}
	item := m.Find(108)
	if _, err := p.setMenuItemFlags(m.Handle, 108, MFGrayed|MFByCommand, MFGrayed|MFDisabled); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ClickMenu(item); err == nil {
		t.Fatal("停用中的項目應該報錯")
	}
	if len(p.Queue) != 0 {
		t.Fatal("被拒絕的點選不該留下訊息")
	}
}
