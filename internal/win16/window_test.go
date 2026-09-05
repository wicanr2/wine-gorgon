package win16

import "testing"

func newTestProcess() *Process {
	p := &Process{
		ScreenW: 640, ScreenH: 480,
		Windows: map[uint16]*Window{},
		Classes: map[string]*Class{},
		Objects: NewObjects(),
	}
	p.Screen = NewSurface(640, 480)
	p.Metrics = defaultMetrics(640, 480)
	p.nextHWnd = 0x0800
	return p
}

// 子視窗的座標是相對父視窗客戶區的。父視窗一移動，子視窗要跟著走——
// 不跟著走的話，被置中過的對話框上的按鈕會留在螢幕外。
func TestChildFollowsParent(t *testing.T) {
	p := newTestProcess()
	parent := &Window{Handle: 0x0800, X: 100, Y: 50, W: 200, H: 100,
		Style: WSCaption | WSDlgFrame}
	child := &Window{Handle: 0x0801, Parent: 0x0800, Style: WSChild,
		X: 10, Y: 20, W: 40, H: 15}
	parent.Children = []uint16{child.Handle}
	p.Windows[parent.Handle] = parent
	p.Windows[child.Handle] = child
	p.layout(parent)

	wantX := 100 + 3 + 10 // 視窗 x ＋ 對話框邊框 ＋ 子視窗相對 x
	wantY := 50 + 3 + 19 + 20
	if child.AbsX != wantX || child.AbsY != wantY {
		t.Fatalf("子視窗在 (%d,%d)，預期 (%d,%d)", child.AbsX, child.AbsY, wantX, wantY)
	}

	parent.X, parent.Y = 0, 0
	p.layout(parent)
	if child.AbsX != 13 || child.AbsY != 42 {
		t.Errorf("父視窗移動後子視窗在 (%d,%d)，預期 (13,42)", child.AbsX, child.AbsY)
	}
	if child.X != 10 || child.Y != 20 {
		t.Errorf("子視窗的相對座標被改動了：(%d,%d)", child.X, child.Y)
	}
}

// 子視窗沒有標題列。WS_CAPTION ＝ WS_BORDER｜WS_DLGFRAME，所以子視窗
// 還是有 3 像素的對話框邊框，但**不該再被吃掉 19 像素的標題列**——
// 照頂層規則算的話，18 像素高的自繪控制項客戶區高度會變成 0，
// 而畫面上只是「什麼都沒有」，不會報錯。
func TestChildHasNoCaptionBar(t *testing.T) {
	p := newTestProcess()
	child := &Window{Handle: 0x0800, Style: WSChild | WSCaption, W: 156, H: 18}
	top := &Window{Handle: 0x0801, Style: WSCaption, W: 156, H: 18}
	p.Windows[child.Handle] = child
	p.Windows[top.Handle] = top
	p.layout(child)
	p.layout(top)
	if child.ClientH != 12 { // 18 − 3 − 3
		t.Errorf("子視窗客戶高 %d，預期 12（只有邊框）", child.ClientH)
	}
	if top.ClientH != 0 { // 18 − 3 − 3 − 19 已經是負的，夾到 0
		t.Errorf("頂層視窗客戶高 %d，預期 0（標題列吃光了）", top.ClientH)
	}
	// 沒有邊框樣式的子視窗整塊都是客戶區——CIV.EXE 的選單項目就是這種。
	plain := &Window{Handle: 0x0802, Style: WSChild, W: 156, H: 18}
	p.Windows[plain.Handle] = plain
	p.layout(plain)
	if plain.ClientW != 156 || plain.ClientH != 18 {
		t.Errorf("無邊框子視窗客戶區 %dx%d，預期 156x18", plain.ClientW, plain.ClientH)
	}
}

// 彈出式視窗（含對話框）的位置是螢幕座標，owner 移動不影響它。
func TestPopupIsNotRelativeToOwner(t *testing.T) {
	p := newTestProcess()
	owner := &Window{Handle: 0x0800, X: 100, Y: 100, W: 300, H: 200}
	popup := &Window{Handle: 0x0801, Parent: 0x0800, X: 10, Y: 10, W: 50, H: 50}
	p.Windows[owner.Handle] = owner
	p.Windows[popup.Handle] = popup
	p.layout(owner)
	p.layout(popup)
	if popup.AbsX != 10 || popup.AbsY != 10 {
		t.Errorf("彈出式視窗在 (%d,%d)，預期 (10,10)", popup.AbsX, popup.AbsY)
	}
}

// 命中測試用螢幕座標，而且後建的在上面。
func TestWindowAtPicksTopmost(t *testing.T) {
	p := newTestProcess()
	for i, h := range []uint16{0x0800, 0x0801} {
		w := &Window{Handle: h, X: 0, Y: 0, W: 100, H: 100, Visible: true}
		w.AbsX, w.AbsY = 0, 0
		_ = i
		p.Windows[h] = w
		p.WindowOrder = append(p.WindowOrder, h)
	}
	got, ok := p.WindowAt(50, 50)
	if !ok || got.Handle != 0x0801 {
		t.Errorf("命中 %v，預期後建的 0801", got)
	}
	if _, ok := p.WindowAt(200, 200); ok {
		t.Error("(200,200) 不該命中任何視窗（沒有桌面時）")
	}
}

// 無效區域取聯集：多畫幾個像素不會錯，少畫才會。
func TestInvalidateUnions(t *testing.T) {
	p := newTestProcess()
	w := &Window{Handle: 0x0800, ClientW: 100, ClientH: 100}
	p.Invalidate(w, &[4]int{10, 10, 20, 20}, false)
	p.Invalidate(w, &[4]int{50, 5, 60, 30}, true)
	if w.InvL != 10 || w.InvT != 5 || w.InvR != 60 || w.InvB != 30 {
		t.Errorf("無效區域 (%d,%d,%d,%d)，預期 (10,5,60,30)", w.InvL, w.InvT, w.InvR, w.InvB)
	}
	if !w.EraseBkg {
		t.Error("第二次要求擦背景，旗標沒立起來")
	}
	p.Validate(w, &[4]int{0, 0, 100, 100})
	if w.NeedPaint {
		t.Error("整塊蓋掉之後應該不必再畫")
	}
}
