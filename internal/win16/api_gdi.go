package win16

// RegisterGDI 登記 GDI 的處理器。
func RegisterGDI(p *Process) {
	h := p.Handlers

	// --- DC 屬性 ---

	h["GDI.#1"] = func(p *Process, a Args) (uint32, error) { // SetBkColor
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		old := d.BkColor
		d.BkColor = a.Long(2)
		return old, nil
	}
	h["GDI.#75"] = func(p *Process, a Args) (uint32, error) { // GetBkColor
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		return d.BkColor, nil
	}
	h["GDI.#2"] = func(p *Process, a Args) (uint32, error) { // SetBkMode
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		old := d.BkMode
		d.BkMode = int(a.Word(2))
		return uint32(old), nil
	}
	h["GDI.#76"] = func(p *Process, a Args) (uint32, error) { // GetBkMode
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		return uint32(d.BkMode), nil
	}
	h["GDI.#9"] = func(p *Process, a Args) (uint32, error) { // SetTextColor
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		old := d.TextColor
		d.TextColor = a.Long(2)
		return old, nil
	}
	h["GDI.#90"] = func(p *Process, a Args) (uint32, error) { // GetTextColor
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		return d.TextColor, nil
	}
	h["GDI.#346"] = func(p *Process, a Args) (uint32, error) { // SetTextAlign
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		old := d.TextAlign
		d.TextAlign = a.Word(2)
		return uint32(old), nil
	}
	h["GDI.#6"] = func(p *Process, a Args) (uint32, error) { // SetPolyFillMode
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		old := d.PolyFill
		d.PolyFill = int(a.Word(2))
		return uint32(old), nil
	}

	// GetDeviceCaps：只回和畫面有關的那幾項。
	h["GDI.#80"] = func(p *Process, a Args) (uint32, error) {
		switch a.Word(2) {
		case 8: // HORZRES
			return uint32(p.ScreenW), nil
		case 10: // VERTRES
			return uint32(p.ScreenH), nil
		case 12: // BITSPIXEL
			return 8, nil
		case 14: // PLANES
			return 1, nil
		case 24: // NUMCOLORS
			return 20, nil
		case 104: // SIZEPALETTE
			return 256, nil
		case 106: // NUMRESERVED
			return 20, nil
		case 88, 90: // LOGPIXELSX／LOGPIXELSY：VGA 是 96 dpi
			return 96, nil
		case 22: // ASPECTX
			return 36, nil
		case 40: // ASPECTY
			return 36, nil
		case 38: // RASTERCAPS
			return 0x0100, nil // RC_PALETTE
		}
		p.note("GetDeviceCaps(%d) 回 0（表上沒有這一項）", a.Word(2))
		return 0, nil
	}

	// --- 畫圖 ---

	h["GDI.#20"] = func(p *Process, a Args) (uint32, error) { // MoveTo
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		old := uint32(uint16(d.CurY))<<16 | uint32(uint16(d.CurX))
		d.CurX, d.CurY = int(int16(a.Word(2))), int(int16(a.Word(4)))
		return old, nil
	}
	h["GDI.#19"] = func(p *Process, a Args) (uint32, error) { // LineTo
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		x, y := int(int16(a.Word(2))), int(int16(a.Word(4)))
		p.drawLine(d, d.CurX, d.CurY, x, y)
		d.CurX, d.CurY = x, y
		return 1, nil
	}
	h["GDI.#31"] = func(p *Process, a Args) (uint32, error) { // SetPixel
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0, nil
		}
		c := a.Long(6)
		d.SetPixel(int(int16(a.Word(2))), int(int16(a.Word(4))), p.colorIndex(c))
		return c, nil
	}
	h["GDI.#83"] = func(p *Process, a Args) (uint32, error) { // GetPixel
		d, ok := p.dc(a.Word(0))
		if !ok {
			return 0xFFFFFFFF, nil
		}
		i := d.Pixel(int(int16(a.Word(2))), int(int16(a.Word(4))))
		e := p.SysPalette[i]
		return uint32(e.B)<<16 | uint32(e.G)<<8 | uint32(e.R), nil
	}
	h["GDI.#37"] = func(p *Process, a Args) (uint32, error) { // Polyline
		return p.poly(a, false)
	}
	h["GDI.#36"] = func(p *Process, a Args) (uint32, error) { // Polygon
		return p.poly(a, true)
	}

	// BitBlt(HDC dst, x, y, w, h, HDC src, sx, sy, DWORD rop)
	h["GDI.#34"] = func(p *Process, a Args) (uint32, error) {
		dst, ok := p.dc(a.Word(0))
		if !ok {
			p.BlitsBadDC++
			p.note("BitBlt 的目的 DC %04X 不存在，呼叫端 %04X:%04X（畫不出來，而且不會報錯）", a.Word(0), p.LastCall.FromCS, p.LastCall.FromIP)
			return 0, nil
		}
		src, srcOK := p.dc(a.Word(10))
		if !srcOK && a.Word(10) != 0 {
			p.note("BitBlt 的來源 DC %04X 不存在", a.Word(10))
		}
		pattern := byte(0)
		if obj, ok := p.Objects.Get(dst.Brush, ObjBrush); ok {
			pattern = obj.Brush.Index
		}
		dx, dy := int(int16(a.Word(2))), int(int16(a.Word(4)))
		bw, bh := int(int16(a.Word(6))), int(int16(a.Word(8)))
		// 大面積、畫到視窗上的 blit 記一筆（去重）：地圖落點對不上時，
		// 要先分清楚是「遊戲傳的座標不同」還是「我們畫錯地方」。
		if p.LogBigBlits && dst.Window != 0 && bw*bh >= 256 {
			p.note("BitBlt → 視窗 %04X 客戶 (%d,%d) %dx%d，來源 %04X (%d,%d)，呼叫端 %04X:%04X",
				dst.Window, dx, dy, bw, bh, a.Word(10),
				int(int16(a.Word(12))), int(int16(a.Word(14))),
				p.LastCall.FromCS, p.LastCall.FromIP)
		}
		BitBlt(dst, dx, dy, bw, bh,
			src, int(int16(a.Word(12))), int(int16(a.Word(14))),
			a.Long(16), pattern)
		p.Blits++
		return 1, nil
	}

	// --- 物件 ---

	h["GDI.#45"] = func(p *Process, a Args) (uint32, error) { return p.selectObject(a.Word(0), a.Word(2)) }
	h["GDI.#69"] = func(p *Process, a Args) (uint32, error) { return boolTo(p.Objects.Delete(a.Word(0))), nil }
	h["GDI.#68"] = func(p *Process, a Args) (uint32, error) { return boolTo(p.Objects.Delete(a.Word(0))), nil }
	h["GDI.#150"] = func(p *Process, _ Args) (uint32, error) { return 1, nil } // UnrealizeObject

	h["GDI.#52"] = func(p *Process, a Args) (uint32, error) { // CreateCompatibleDC
		d := &DC{Surf: NewSurface(1, 1), BkMode: 2}
		d.ClipR, d.ClipB = 1, 1
		d.Font = p.stockObject(13)
		hdc := p.Objects.Add(&Object{Kind: ObjDC, DC: d})
		d.Handle = hdc
		return uint32(hdc), nil
	}

	// CreateBitmap(w, h, planes, bpp, const void far* bits)
	h["GDI.#48"] = func(p *Process, a Args) (uint32, error) {
		w, hgt := int(int16(a.Word(0))), int(int16(a.Word(2)))
		planes, bpp := uint8(a.Word(4)), uint8(a.Word(6))
		p.BitmapKinds[[2]uint8{planes, bpp}]++
		if planes != 1 || (bpp != 8 && bpp != 1) {
			p.note("CreateBitmap %d 平面 %d bpp（只做 1 平面的 1 或 8 bpp）", planes, bpp)
		}
		bm := &Bitmap{Surf: NewSurface(w, hgt), Planes: planes, BPP: bpp}
		if sel, off := a.Ptr(8); sel != 0 {
			p.loadBitmapBits(bm, sel, off)
		}
		return uint32(p.Objects.Add(&Object{Kind: ObjBitmap, Bitmap: bm})), nil
	}

	// GetBitmapBits(HBITMAP, LONG count, void far* bits)
	h["GDI.#74"] = func(p *Process, a Args) (uint32, error) {
		obj, ok := p.Objects.Get(a.Word(0), ObjBitmap)
		if !ok {
			return 0, nil
		}
		sel, off := a.Ptr(6)
		n := int(a.Long(2))
		return uint32(p.storeBitmapBits(obj.Bitmap, sel, off, n)), nil
	}

	// SetBitmapBits(HBITMAP, DWORD count, const void far* bits)
	h["GDI.#106"] = func(p *Process, a Args) (uint32, error) {
		obj, ok := p.Objects.Get(a.Word(0), ObjBitmap)
		if !ok {
			return 0, nil
		}
		sel, off := a.Ptr(6)
		return uint32(p.loadBitmapBitsN(obj.Bitmap, sel, off, int(a.Long(2)))), nil
	}

	h["GDI.#162"] = func(p *Process, a Args) (uint32, error) { // GetBitmapDimension
		obj, ok := p.Objects.Get(a.Word(0), ObjBitmap)
		if !ok {
			return 0, nil
		}
		return uint32(uint16(obj.Bitmap.DimY))<<16 | uint32(uint16(obj.Bitmap.DimX)), nil
	}
	h["GDI.#163"] = func(p *Process, a Args) (uint32, error) { // SetBitmapDimension
		obj, ok := p.Objects.Get(a.Word(0), ObjBitmap)
		if !ok {
			return 0, nil
		}
		old := uint32(uint16(obj.Bitmap.DimY))<<16 | uint32(uint16(obj.Bitmap.DimX))
		obj.Bitmap.DimX, obj.Bitmap.DimY = int(int16(a.Word(2))), int(int16(a.Word(4)))
		return old, nil
	}

	h["GDI.#66"] = func(p *Process, a Args) (uint32, error) { // CreateSolidBrush
		c := a.Long(0)
		return uint32(p.Objects.Add(&Object{Kind: ObjBrush,
			Brush: &Brush{Color: c, Index: p.colorIndex(c)}})), nil
	}
	h["GDI.#60"] = func(p *Process, a Args) (uint32, error) { // CreatePatternBrush
		return uint32(p.Objects.Add(&Object{Kind: ObjBrush,
			Brush: &Brush{Patt: a.Word(0)}})), nil
	}
	h["GDI.#61"] = func(p *Process, a Args) (uint32, error) { // CreatePen(style, width, color)
		c := a.Long(4)
		return uint32(p.Objects.Add(&Object{Kind: ObjPen, Pen: &Pen{
			Style: int(a.Word(0)), Width: int(int16(a.Word(2))),
			Color: c, Index: p.colorIndex(c)}})), nil
	}
	h["GDI.#87"] = func(p *Process, a Args) (uint32, error) { return uint32(p.stockObject(a.Word(0))), nil }

	// --- 調色盤 ---

	// CreatePalette(const LOGPALETTE far*)：+0 version(2) +2 count(2)
	// 之後每格 4 個 byte（R G B flags）。
	h["GDI.#360"] = func(p *Process, a Args) (uint32, error) {
		sel, off := a.Ptr(0)
		n, _ := p.Mod.Mem.ReadU16(sel, off+2)
		pal := &Palette{Entries: make([]RGB, n), Flags: make([]uint8, n)}
		for i := 0; i < int(n); i++ {
			b := off + 4 + uint16(i*4)
			r, _ := p.Mod.Mem.ReadU8(sel, b)
			g, _ := p.Mod.Mem.ReadU8(sel, b+1)
			bl, _ := p.Mod.Mem.ReadU8(sel, b+2)
			fl, _ := p.Mod.Mem.ReadU8(sel, b+3)
			pal.Entries[i] = RGB{r, g, bl}
			pal.Flags[i] = fl
		}
		return uint32(p.Objects.Add(&Object{Kind: ObjPalette, Palette: pal})), nil
	}

	// SetPaletteEntries(HPALETTE, UINT start, UINT count, const PALETTEENTRY far*)
	h["GDI.#364"] = func(p *Process, a Args) (uint32, error) { return p.setPaletteEntries(a, false) }
	// AnimatePalette 的參數形狀相同，差別是它直接動實體調色盤。
	h["GDI.#367"] = func(p *Process, a Args) (uint32, error) { return p.setPaletteEntries(a, true) }

	// GetSystemPaletteEntries(HDC, UINT start, UINT count, PALETTEENTRY far*)
	h["GDI.#375"] = func(p *Process, a Args) (uint32, error) {
		start, count := int(a.Word(2)), int(a.Word(4))
		sel, off := a.Ptr(6)
		n := 0
		for i := 0; i < count && start+i < 256; i++ {
			e := p.SysPalette[start+i]
			b := off + uint16(i*4)
			_ = p.Mod.Mem.WriteU8(sel, b, e.R)
			_ = p.Mod.Mem.WriteU8(sel, b+1, e.G)
			_ = p.Mod.Mem.WriteU8(sel, b+2, e.B)
			_ = p.Mod.Mem.WriteU8(sel, b+3, 0)
			n++
		}
		return uint32(n), nil
	}

	// --- 字型與文字：只記，不畫（見 §字型）---

	h["GDI.#56"] = func(p *Process, a Args) (uint32, error) { // CreateFont
		sel, off := a.Ptr(26)
		f := &Font{
			Height: int(int16(a.Word(0))), Width: int(int16(a.Word(2))),
			Weight: int(int16(a.Word(8))), Italic: a.Word(10) != 0,
		}
		if sel != 0 {
			f.FaceName = p.CString(sel, off)
		}
		f.Bitmap = p.matchFont(f)
		return uint32(p.Objects.Add(&Object{Kind: ObjFont, Font: f})), nil
	}
	h["GDI.#57"] = func(p *Process, a Args) (uint32, error) { // CreateFontIndirect(LOGFONT far*)
		sel, off := a.Ptr(0)
		hgt, _ := p.Mod.Mem.ReadU16(sel, off)
		wid, _ := p.Mod.Mem.ReadU16(sel, off+2)
		wgt, _ := p.Mod.Mem.ReadU16(sel, off+8)
		f := &Font{Height: int(int16(hgt)), Width: int(int16(wid)), Weight: int(int16(wgt))}
		f.FaceName = p.CString(sel, off+20)
		f.Bitmap = p.matchFont(f)
		return uint32(p.Objects.Add(&Object{Kind: ObjFont, Font: f})), nil
	}
	h["GDI.#33"] = func(p *Process, a Args) (uint32, error) { // TextOut
		sel, off := a.Ptr(6)
		n := int(int16(a.Word(10)))
		var b []byte
		for i := 0; i < n; i++ {
			v, _ := p.Mod.Mem.ReadU8(sel, off+uint16(i))
			b = append(b, v)
		}
		x, y := int(int16(a.Word(2))), int(int16(a.Word(4)))
		call := TextOutCall{DC: a.Word(0), X: x, Y: y, Text: string(b), Steps: p.CPU.Steps}
		d, ok := p.dc(a.Word(0))
		if ok {
			call.ScreenX, call.ScreenY = x+d.OrgX, y+d.OrgY
			call.Window = d.Window
			p.drawText(d, x, y, string(b))
		}
		p.TextOuts = append(p.TextOuts, call)
		return 1, nil
	}
	h["GDI.#91"] = func(p *Process, a Args) (uint32, error) { // GetTextExtent(HDC, LPCSTR, int)
		sel, off := a.Ptr(2)
		n := int(int16(a.Word(6)))
		d, _ := p.dc(a.Word(0))
		if f := p.dcFont(d); f != nil {
			var s []byte
			for i := 0; i < n; i++ {
				v, _ := p.Mod.Mem.ReadU8(sel, off+uint16(i))
				s = append(s, v)
			}
			return uint32(uint16(f.Height))<<16 | uint32(uint16(f.TextWidth(string(s)))), nil
		}
		w, hgt := p.textExtent(a.Word(0), n)
		return uint32(uint16(hgt))<<16 | uint32(uint16(w)), nil
	}
	// GetTextMetrics(HDC, TEXTMETRIC far*)：Win16 的 TEXTMETRIC 前六個
	// 欄位是 tmHeight／tmAscent／tmDescent／tmInternalLeading／
	// tmExternalLeading／tmAveCharWidth，各兩個 byte。
	// civ1 `docs/re/319` 靠 tmAscent 判斷文字陰影要不要 +1，所以這幾個值
	// 不能亂填。
	h["GDI.#93"] = func(p *Process, a Args) (uint32, error) {
		sel, off := a.Ptr(2)
		d, _ := p.dc(a.Word(0))
		put := func(o uint16, v uint16) { _ = p.Mod.Mem.WriteU16(sel, off+o, v) }
		if f := p.dcFont(d); f != nil {
			put(0, uint16(f.Height))
			put(2, uint16(f.Ascent))
			put(4, uint16(f.Height-f.Ascent))
			put(6, uint16(f.IntLeading))
			put(8, uint16(f.ExtLeading))
			put(10, uint16(f.AvgWidth))
			put(12, uint16(f.MaxWidth))
			put(14, uint16(f.Weight))
			return 1, nil
		}
		put(0, 16)
		put(2, 12)
		put(4, 4)
		put(10, 8)
		put(12, 8)
		return 1, nil
	}
	// EnumFontFamilies(HDC, LPCSTR family, FONTENUMPROC, LPARAM)
	//
	// 回呼收到 (LOGFONT far*, TEXTMETRIC far*, int type, LPARAM)。
	// **列舉順序就是字型檔裡的順序**——civ1 `docs/re/319` 靠這一點認出
	// CIVTIMES18 是第 17 個 face，所以順序不能亂。
	h["GDI.#330"] = func(p *Process, a Args) (uint32, error) {
		famSel, famOff := a.Ptr(2)
		family := ""
		if famSel != 0 {
			family = p.CString(famSel, famOff)
		}
		procSel, procOff := a.Ptr(6)
		lParam := a.Long(10)
		if procSel == 0 && procOff == 0 {
			return 0, nil
		}
		blk := p.Mod.Mem.Alloc("EnumFonts 暫存", 128)
		if blk == nil {
			return 0, errUnsupported("EnumFontFamilies 配不到暫存 selector")
		}
		defer p.Mod.Mem.Free(blk.Sel)
		n := uint32(0)
		for _, f := range p.Fonts {
			if family != "" && !equalFoldASCII(f.Face, family) {
				continue
			}
			p.writeLogFont(blk.Sel, 0, f)
			p.writeTextMetric(blk.Sel, 50, f)
			n++
			r, err := p.Call16(procSel, procOff,
				blk.Sel, 0, blk.Sel, 50, 0, // lplf、lpntm、FontType ＝ 0（點陣）
				uint16(lParam>>16), uint16(lParam))
			if err != nil {
				return 0, err
			}
			if r == 0 {
				break
			}
		}
		return n, nil
	}
	h["GDI.#119"] = func(p *Process, a Args) (uint32, error) { // AddFontResource
		sel, off := a.Ptr(0)
		name := p.CString(sel, off)
		p.FontFiles = append(p.FontFiles, name)
		n, err := p.LoadFontFile(name)
		if err != nil {
			p.note("AddFontResource %q 失敗：%v", name, err)
			return 0, nil
		}
		return uint32(n), nil
	}
	h["GDI.#136"] = func(p *Process, _ Args) (uint32, error) { return 1, nil } // RemoveFontResource
}

// TextOutCall 是一次 TextOut。畫不出來的字先記著——它們是「還缺什麼」
// 的清單，也是之後接上字型時的回歸樣本。
type TextOutCall struct {
	DC     uint16
	Window uint16
	X, Y   int // DC 座標
	// ScreenX／ScreenY 是螢幕座標。字還畫不出來的時候，這一串就是
	// 唯一看得到畫面內容的東西——用它導航對話框。
	ScreenX, ScreenY int
	Text             string
	Steps            uint64
}

// dcFont 取這個 DC 目前選著的點陣字面；沒有就回 nil。
func (p *Process) dcFont(d *DC) *BitmapFont {
	if d == nil {
		return nil
	}
	if obj, ok := p.Objects.Get(d.Font, ObjFont); ok {
		return obj.Font.Bitmap
	}
	return nil
}

// drawText 把一串字畫上去。
//
// 座標是**左上角**（TA_LEFT｜TA_TOP，Windows 的預設）；`TA_BASELINE`
// 時 y 是基線，要往上退一個 ascent。背景模式 OPAQUE 要先填底色。
func (p *Process) drawText(d *DC, x, y int, s string) {
	f := p.dcFont(d)
	if f == nil || s == "" {
		if f == nil {
			p.note("TextOut 沒有可用的點陣字面（字畫不出來）")
		}
		return
	}
	const taBaseline = 24 // TA_BASELINE ＝ 24（TA_BOTTOM｜TA_TOP 的組合值）
	top := y
	if d.TextAlign&taBaseline == taBaseline {
		top = y - f.Ascent
	}
	if d.TextAlign&6 == 2 { // TA_RIGHT
		x -= f.TextWidth(s)
	} else if d.TextAlign&6 == 6 { // TA_CENTER
		x -= f.TextWidth(s) / 2
	}
	fg := p.colorIndex(d.TextColor)
	bg := p.colorIndex(d.BkColor)
	const opaque = 2
	if d.BkMode == opaque {
		d.FillRect(x, top, f.TextWidth(s), f.Height, bg)
	}
	cx := x
	for i := 0; i < len(s); i++ {
		c := s[i]
		w := f.CharWidth(c)
		for gy := 0; gy < f.Height; gy++ {
			for gx := 0; gx < w; gx++ {
				if f.Pixel(c, gx, gy) {
					d.SetPixel(cx+gx, top+gy, fg)
				}
			}
		}
		cx += w
	}
}

// matchFont 依 LOGFONT 挑一個載入好的字面。
//
// 規則刻意簡單：**字面名優先，其次高度最接近**。原版 Windows 的字型
// 對應規則複雜得多，但 CIV.EXE 只用它自己 `AddFontResource` 進來的那幾個，
// 名字對得上就沒有歧義。
func (p *Process) matchFont(want *Font) *BitmapFont {
	if len(p.Fonts) == 0 {
		return nil
	}
	h := want.Height
	if h < 0 {
		h = -h
	}
	var best *BitmapFont
	bestScore := 1 << 30
	for _, f := range p.Fonts {
		score := 0
		if want.FaceName != "" && !equalFoldASCII(f.Face, want.FaceName) {
			score += 10000
		}
		d := f.Height - h
		if d < 0 {
			d = -d
		}
		score += d * 10
		if want.Weight >= 700 != (f.Weight >= 700) {
			score += 5
		}
		if score < bestScore {
			best, bestScore = f, score
		}
	}
	return best
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'a' && x <= 'z' {
			x -= 32
		}
		if y >= 'a' && y <= 'z' {
			y -= 32
		}
		if x != y {
			return false
		}
	}
	return true
}

// writeLogFont 把一個字面寫成 Win16 的 LOGFONT（50 個 byte）。
func (p *Process) writeLogFont(sel, off uint16, f *BitmapFont) {
	put16 := func(o uint16, v uint16) { _ = p.Mod.Mem.WriteU16(sel, off+o, v) }
	put8 := func(o uint16, v uint8) { _ = p.Mod.Mem.WriteU8(sel, off+o, v) }
	put16(0, uint16(f.Height))
	put16(2, uint16(f.AvgWidth))
	put16(4, 0)
	put16(6, 0)
	put16(8, uint16(f.Weight))
	put8(10, boolByte(f.Italic))
	put8(11, 0)
	put8(12, 0)
	put8(13, 0)
	put8(14, 0)
	put8(15, 0)
	put8(16, 0)
	put8(17, 0)
	for i := 0; i < 32; i++ {
		var c byte
		if i < len(f.Face) {
			c = f.Face[i]
		}
		put8(18+uint16(i), c)
	}
}

// writeTextMetric 把一個字面寫成 Win16 的 TEXTMETRIC（31 個 byte）。
func (p *Process) writeTextMetric(sel, off uint16, f *BitmapFont) {
	put16 := func(o uint16, v uint16) { _ = p.Mod.Mem.WriteU16(sel, off+o, v) }
	put8 := func(o uint16, v uint8) { _ = p.Mod.Mem.WriteU8(sel, off+o, v) }
	put16(0, uint16(f.Height))
	put16(2, uint16(f.Ascent))
	put16(4, uint16(f.Height-f.Ascent))
	put16(6, uint16(f.IntLeading))
	put16(8, uint16(f.ExtLeading))
	put16(10, uint16(f.AvgWidth))
	put16(12, uint16(f.MaxWidth))
	put16(14, uint16(f.Weight))
	put8(16, boolByte(f.Italic))
	put8(17, 0)
	put8(18, 0)
	put8(19, f.FirstChar)
	put8(20, f.LastChar)
	put8(21, f.DefChar)
	put8(22, f.BreakChar)
	put8(23, 0)
	put8(24, 0)
	put16(25, 0)
	put16(27, 36)
	put16(29, 36)
}

func boolByte(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

func (p *Process) textExtent(hdc uint16, n int) (int, int) {
	d, _ := p.dc(hdc)
	if f := p.dcFont(d); f != nil {
		return f.AvgWidth * n, f.Height
	}
	return 8 * n, 16
}

func (p *Process) setPaletteEntries(a Args, animate bool) (uint32, error) {
	obj, ok := p.Objects.Get(a.Word(0), ObjPalette)
	if !ok {
		return 0, nil
	}
	start, count := int(a.Word(2)), int(a.Word(4))
	sel, off := a.Ptr(6)
	n := 0
	for i := 0; i < count; i++ {
		idx := start + i
		if idx >= len(obj.Palette.Entries) {
			break
		}
		b := off + uint16(i*4)
		r, _ := p.Mod.Mem.ReadU8(sel, b)
		g, _ := p.Mod.Mem.ReadU8(sel, b+1)
		bl, _ := p.Mod.Mem.ReadU8(sel, b+2)
		obj.Palette.Entries[idx] = RGB{r, g, bl}
		if idx < len(obj.Palette.Flags) {
			fl, _ := p.Mod.Mem.ReadU8(sel, b+3)
			obj.Palette.Flags[idx] = fl
		}
		// AnimatePalette 動的是**這一份**調色盤自己的實體格子。
		if animate && idx < len(obj.Palette.Map) {
			p.SysPalette[obj.Palette.Map[idx]] = RGB{r, g, bl}
		}
		n++
	}
	return uint32(n), nil
}

// selectObject 把一個物件選進 DC，回傳被換掉的那個。
func (p *Process) selectObject(hdc, hobj uint16) (uint32, error) {
	d, ok := p.dc(hdc)
	if !ok {
		return 0, nil
	}
	obj, ok := p.Objects.Any(hobj)
	if !ok {
		return 0, nil
	}
	var old uint16
	switch obj.Kind {
	case ObjBitmap:
		old, d.Bitmap = d.Bitmap, hobj
		// 記憶體 DC 選進點陣圖 ＝ 換掉它畫的那塊畫布。
		d.Surf = obj.Bitmap.Surf
		d.OrgX, d.OrgY = 0, 0
		d.ClipL, d.ClipT = 0, 0
		d.ClipR, d.ClipB = obj.Bitmap.Surf.W, obj.Bitmap.Surf.H
	case ObjBrush:
		old, d.Brush = d.Brush, hobj
	case ObjPen:
		old, d.Pen = d.Pen, hobj
	case ObjFont:
		old, d.Font = d.Font, hobj
	case ObjPalette:
		old, d.Pal = d.Pal, hobj
	}
	return uint32(old), nil
}

// stockObject 回傳系統物件（GetStockObject）。
func (p *Process) stockObject(i uint16) uint16 {
	if p.stock == nil {
		p.stock = map[uint16]uint16{}
	}
	if h, ok := p.stock[i]; ok {
		return h
	}
	var obj *Object
	switch i {
	case 0, 1, 2, 3, 4: // WHITE/LTGRAY/GRAY/DKGRAY/BLACK_BRUSH
		colors := []uint32{0xFFFFFF, 0xC0C0C0, 0x808080, 0x404040, 0x000000}
		c := colors[i]
		obj = &Object{Kind: ObjBrush, Brush: &Brush{Color: c, Index: p.colorIndex(c)}, Stock: true}
	case 5: // NULL_BRUSH
		obj = &Object{Kind: ObjBrush, Brush: &Brush{Hollow: true}, Stock: true}
	case 6, 7: // WHITE/BLACK_PEN
		c := uint32(0xFFFFFF)
		if i == 7 {
			c = 0
		}
		obj = &Object{Kind: ObjPen, Pen: &Pen{Width: 1, Color: c, Index: p.colorIndex(c)}, Stock: true}
	case 8: // NULL_PEN
		obj = &Object{Kind: ObjPen, Pen: &Pen{Style: 5}, Stock: true}
	default: // 字型一族
		f := &Font{Height: 16, Width: 8, FaceName: "System"}
		f.Bitmap = p.matchFont(f)
		obj = &Object{Kind: ObjFont, Font: f, Stock: true}
	}
	h := p.Objects.Add(obj)
	p.stock[i] = h
	return h
}

// drawLine 是 Bresenham；GDI 的線**不畫終點**。
func (p *Process) drawLine(d *DC, x0, y0, x1, y1 int) {
	idx := byte(0)
	if obj, ok := p.Objects.Get(d.Pen, ObjPen); ok {
		if obj.Pen.Style == 5 { // PS_NULL
			return
		}
		idx = obj.Pen.Index
	}
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		if x0 == x1 && y0 == y1 {
			return
		}
		d.SetPixel(x0, y0, idx)
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func (p *Process) poly(a Args, closed bool) (uint32, error) {
	d, ok := p.dc(a.Word(0))
	if !ok {
		return 0, nil
	}
	sel, off := a.Ptr(2)
	n := int(int16(a.Word(6)))
	if n < 2 {
		return 0, nil
	}
	pt := func(i int) (int, int) {
		x, _ := p.Mod.Mem.ReadU16(sel, off+uint16(i*4))
		y, _ := p.Mod.Mem.ReadU16(sel, off+uint16(i*4+2))
		return int(int16(x)), int(int16(y))
	}
	x0, y0 := pt(0)
	for i := 1; i < n; i++ {
		x, y := pt(i)
		p.drawLine(d, x0, y0, x, y)
		x0, y0 = x, y
	}
	if closed {
		fx, fy := pt(0)
		p.drawLine(d, x0, y0, fx, fy)
		p.note("Polygon 只畫外框沒填內部（還沒做掃描線填色）")
	}
	return 1, nil
}

// loadBitmapBits 把 DDB 的位元組讀進 Surface。
//
// GDI 的 DDB 每列是**字組對齊**，8bpp 就是補到偶數——和 Surface.Stride
// 的算法一致，所以可以整列複製。
func (p *Process) loadBitmapBits(bm *Bitmap, sel, off uint16) int {
	return p.loadBitmapBitsN(bm, sel, off, bm.Surf.Stride*bm.Surf.H)
}

func (p *Process) loadBitmapBitsN(bm *Bitmap, sel, off uint16, n int) int {
	total := bm.Surf.Stride * bm.Surf.H
	if n > total {
		n = total
	}
	for i := 0; i < n; i++ {
		v, err := p.Mod.Mem.ReadU8(sel, off+uint16(i))
		if err != nil {
			return i
		}
		bm.Surf.Bits[i] = v
	}
	return n
}

func (p *Process) storeBitmapBits(bm *Bitmap, sel, off uint16, n int) int {
	total := bm.Surf.Stride * bm.Surf.H
	if n > total || n <= 0 {
		n = total
	}
	for i := 0; i < n; i++ {
		if err := p.Mod.Mem.WriteU8(sel, off+uint16(i), bm.Surf.Bits[i]); err != nil {
			return i
		}
	}
	return n
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
