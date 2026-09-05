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

	wantX := 100 + 4 + 10 // 視窗 x ＋ 邊框（SM_CXFRAME）＋ 子視窗相對 x
	wantY := 50 + 4 + 19 + 20
	if child.AbsX != wantX || child.AbsY != wantY {
		t.Fatalf("子視窗在 (%d,%d)，預期 (%d,%d)", child.AbsX, child.AbsY, wantX, wantY)
	}

	parent.X, parent.Y = 0, 0
	p.layout(parent)
	if child.AbsX != 14 || child.AbsY != 43 {
		t.Errorf("父視窗移動後子視窗在 (%d,%d)，預期 (14,43)", child.AbsX, child.AbsY)
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

// 同一個 API 不能被兩個模組各登記一次：後面的會蓋掉前面的，而症狀是
// 「畫面正常但再也不動」。IsDialogMessage 就這樣被蓋掉過。
func TestNoDuplicateHandlerRegistrations(t *testing.T) {
	if dup := DuplicateHandlerKeys(); len(dup) > 0 {
		for k, names := range dup {
			t.Errorf("%s 被 %v 各登記一次", k, names)
		}
	}
}

// selector 配置器不能繞回段的號段。早期版本用 `next += 8`，配到第
// 8,192 個時 uint16 溢位變成 0x0007／0x000F，也就是段 0、段 1——
// 一塊 GlobalAlloc 把程式碼段整個蓋掉，而症狀是幾千萬條指令之後的
// 「取指失敗」，離肇因非常遠。
func TestSelectorAllocatorNeverHitsSegmentSpace(t *testing.T) {
	m := NewMemory()
	m.Put(SegSelector(1), "seg 1", make([]byte, 16))
	seen := map[uint16]bool{}
	for i := 0; i < 5000; i++ {
		b := m.Alloc("x", 1)
		if b == nil {
			t.Fatalf("第 %d 次就配不出來了", i)
		}
		if b.Sel < 0x8000 {
			t.Fatalf("第 %d 次配到 %04X，落進段的號段", i, b.Sel)
		}
		if seen[b.Sel] {
			t.Fatalf("第 %d 次重複配到 %04X", i, b.Sel)
		}
		seen[b.Sel] = true
		m.Free(b.Sel) // 釋放之後要能重用，否則 4,000 多個就用完了
		delete(seen, b.Sel)
	}
	if blk, ok := m.Block(SegSelector(1)); !ok || blk.Name != "seg 1" {
		t.Error("段 1 被蓋掉了")
	}
}

func TestSelectorAllocatorReportsExhaustion(t *testing.T) {
	m := NewMemory()
	n := 0
	for {
		b := m.Alloc("x", 1)
		if b == nil {
			break
		}
		n++
		if n > 20000 {
			t.Fatal("配不完——耗盡時應該回 nil")
		}
	}
	if n < 1000 {
		t.Errorf("只配得出 %d 個 selector，太少", n)
	}
}

// GWW_ID 對子視窗要回控制項編號。CreateWindow 的 hMenu 對子視窗是編號、
// 對頂層視窗是選單 handle；讀的時候不分開的話，控制項送出的 WM_COMMAND
// 會帶 wParam=0，父視窗分不出是哪一個按鈕被按了。
func TestGetWindowWordIDSplitsChildAndMenu(t *testing.T) {
	p := newTestProcess()
	child := &Window{Handle: 0x0800, Style: WSChild, CtrlID: 501}
	top := &Window{Handle: 0x0801, Menu: 0x0300}
	p.Windows[child.Handle] = child
	p.Windows[top.Handle] = top

	const gwwID = -12
	if got, _ := p.windowWord(child.Handle, gwwID, nil); got != 501 {
		t.Errorf("子視窗的 GWW_ID = %d，預期 501", got)
	}
	if got, _ := p.windowWord(top.Handle, gwwID, nil); got != 0x0300 {
		t.Errorf("頂層視窗的 GWW_ID = %04X，預期 0300", got)
	}
}

// 視窗框線的尺寸是從原版參考幀量出來的，不是文件上的常見值。
// 主視窗在 (168,0) 632×600 時，客戶區必須是 (171,41) 610×540。
func TestMainWindowClientMatchesOracle(t *testing.T) {
	p := newTestProcess()
	p.ScreenW, p.ScreenH = 800, 600
	p.Metrics = defaultMetrics(800, 600)
	w := &Window{
		Handle: 0x0800,
		Style:  WSCaption | WSThickFram | WSVScroll | WSHScroll,
		X:      168, Y: 0, W: 632, H: 600,
		HasMenu: true,
	}
	p.Windows[w.Handle] = w
	p.layout(w)
	// (172,42) 是**內容原點**，不是「量到的黑區左上角」。參考幀上黑區
	// 從 (171,41) 起，但那一列與那一行是主窗框最後一道黑線——框是黑的、
	// 未揭露的地圖格也是黑的，量的時候分不出來（civ1 DS327 §3）。
	// 608 ＝ 19 格 × 32，與遊戲自己算的視窗寬（608 ＋ 2×4 ＋ 16 捲軸
	// ＝ 632）閉合。
	if w.ClientX != 172 || w.ClientY != 42 {
		t.Errorf("客戶區原點 (%d,%d)，預期 (172,42)", w.ClientX, w.ClientY)
	}
	if w.ClientW != 608 || w.ClientH != 538 {
		t.Errorf("客戶區大小 %dx%d，預期 608x538", w.ClientW, w.ClientH)
	}
}

// TestWindowDCFollowsMoveWindow 鎖住「視窗 DC 的幾何跟著視窗走」。
//
// CIV.EXE 先用一個小尺寸 CreateWindow（`WdwSmMap` 40×40、`CIV` 600×400），
// 拿到 DC，**之後才 MoveWindow 到最終大小**，然後用同一個 DC 畫。把
// GetDC 當下的幾何凍住的話：小地圖整片是黑的（畫的東西被裁到建立時的
// 35×16 裡），主地圖的內容整個往左偏 168 px（原點停在建立時的客戶區）。
// 兩個症狀都真的發生過，而且各自被當成兩個不同的謎。
func TestWindowDCFollowsMoveWindow(t *testing.T) {
	p := newTestProcess()
	p.ScreenW, p.ScreenH = 800, 600
	p.Screen = NewSurface(800, 600)
	p.Metrics = defaultMetrics(800, 600)
	w := &Window{
		Handle: 0x0800,
		Style:  WSCaption | WSThickFram | WSVScroll | WSHScroll,
		X:      0, Y: 0, W: 600, H: 400,
		HasMenu: true,
	}
	p.Windows[w.Handle] = w
	p.layout(w)
	h := p.newWindowDC(w, nil)

	w.X, w.Y, w.W, w.H = 168, 0, 632, 600
	p.layout(w)

	d, ok := p.dc(h)
	if !ok {
		t.Fatal("取不到 DC")
	}
	if d.OrgX != w.ClientX || d.OrgY != w.ClientY {
		t.Errorf("DC 原點 (%d,%d)，視窗客戶區是 (%d,%d)",
			d.OrgX, d.OrgY, w.ClientX, w.ClientY)
	}
	if d.ClipL != w.ClientX || d.ClipT != w.ClientY ||
		d.ClipR != w.ClientX+w.ClientW || d.ClipB != w.ClientY+w.ClientH {
		t.Errorf("DC 裁剪 (%d,%d,%d,%d)，客戶區是 (%d,%d,%d,%d)",
			d.ClipL, d.ClipT, d.ClipR, d.ClipB,
			w.ClientX, w.ClientY, w.ClientX+w.ClientW, w.ClientY+w.ClientH)
	}
}

// TestPaintDCClipStaysRelative 是上一支的另一半：BeginPaint 給的更新矩形
// 是**相對客戶區**的，視窗移動之後要跟著移，不能留在絕對座標上。
func TestPaintDCClipStaysRelative(t *testing.T) {
	p := newTestProcess()
	p.ScreenW, p.ScreenH = 800, 600
	p.Metrics = defaultMetrics(800, 600)
	w := &Window{Handle: 0x0800, X: 0, Y: 0, W: 200, H: 200}
	p.Windows[w.Handle] = w
	p.layout(w)
	clip := [4]int{10, 10, 50, 50}
	h := p.newWindowDC(w, &clip)

	w.X, w.Y = 100, 80
	p.layout(w)
	d, _ := p.dc(h)
	if d.ClipL != w.ClientX+10 || d.ClipT != w.ClientY+10 {
		t.Errorf("更新矩形左上 (%d,%d)，預期 (%d,%d)",
			d.ClipL, d.ClipT, w.ClientX+10, w.ClientY+10)
	}
}
