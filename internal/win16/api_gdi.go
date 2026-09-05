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
			return 0, nil
		}
		src, _ := p.dc(a.Word(12))
		pattern := byte(0)
		if obj, ok := p.Objects.Get(dst.Brush, ObjBrush); ok {
			pattern = obj.Brush.Index
		}
		BitBlt(dst, int(int16(a.Word(2))), int(int16(a.Word(4))),
			int(int16(a.Word(6))), int(int16(a.Word(8))),
			src, int(int16(a.Word(14))), int(int16(a.Word(16))),
			a.Long(18), pattern)
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
		hdc := p.Objects.Add(&Object{Kind: ObjDC, DC: d})
		d.Handle = hdc
		return uint32(hdc), nil
	}

	// CreateBitmap(w, h, planes, bpp, const void far* bits)
	h["GDI.#48"] = func(p *Process, a Args) (uint32, error) {
		w, hgt := int(int16(a.Word(0))), int(int16(a.Word(2)))
		planes, bpp := uint8(a.Word(4)), uint8(a.Word(6))
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
		pal := &Palette{Entries: make([]RGB, n)}
		for i := 0; i < int(n); i++ {
			b := off + 4 + uint16(i*4)
			r, _ := p.Mod.Mem.ReadU8(sel, b)
			g, _ := p.Mod.Mem.ReadU8(sel, b+1)
			bl, _ := p.Mod.Mem.ReadU8(sel, b+2)
			pal.Entries[i] = RGB{r, g, bl}
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
		return uint32(p.Objects.Add(&Object{Kind: ObjFont, Font: f})), nil
	}
	h["GDI.#57"] = func(p *Process, a Args) (uint32, error) { // CreateFontIndirect(LOGFONT far*)
		sel, off := a.Ptr(0)
		hgt, _ := p.Mod.Mem.ReadU16(sel, off)
		wid, _ := p.Mod.Mem.ReadU16(sel, off+2)
		wgt, _ := p.Mod.Mem.ReadU16(sel, off+8)
		f := &Font{Height: int(int16(hgt)), Width: int(int16(wid)), Weight: int(int16(wgt))}
		f.FaceName = p.CString(sel, off+20)
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
		p.TextOuts = append(p.TextOuts, TextOutCall{
			DC: a.Word(0), X: int(int16(a.Word(2))), Y: int(int16(a.Word(4))),
			Text: string(b), Steps: p.CPU.Steps,
		})
		p.note("TextOut 只記錄不畫字（還沒接原版點陣字型）")
		return 1, nil
	}
	h["GDI.#91"] = func(p *Process, a Args) (uint32, error) { // GetTextExtent
		n := int(int16(a.Word(6)))
		w, hgt := p.textExtent(a.Word(0), n)
		return uint32(uint16(hgt))<<16 | uint32(uint16(w)), nil
	}
	h["GDI.#93"] = func(p *Process, a Args) (uint32, error) { // GetTextMetrics
		sel, off := a.Ptr(2)
		_, hgt := p.textExtent(a.Word(0), 1)
		put := func(d uint16, v uint16) { _ = p.Mod.Mem.WriteU16(sel, off+d, v) }
		put(0, uint16(hgt))         // tmHeight
		put(2, uint16(hgt*3/4))     // tmAscent
		put(4, uint16(hgt-hgt*3/4)) // tmDescent
		put(10, 8)                  // tmAveCharWidth
		put(12, 8)                  // tmMaxCharWidth
		return 1, nil
	}
	h["GDI.#330"] = func(p *Process, _ Args) (uint32, error) { // EnumFontFamilies
		p.note("EnumFontFamilies 回 0（還沒接字型檔）")
		return 0, nil
	}
	h["GDI.#119"] = func(p *Process, a Args) (uint32, error) { // AddFontResource
		sel, off := a.Ptr(0)
		p.FontFiles = append(p.FontFiles, p.CString(sel, off))
		return 1, nil
	}
	h["GDI.#136"] = func(p *Process, _ Args) (uint32, error) { return 1, nil } // RemoveFontResource
}

// TextOutCall 是一次 TextOut。畫不出來的字先記著——它們是「還缺什麼」
// 的清單，也是之後接上字型時的回歸樣本。
type TextOutCall struct {
	DC    uint16
	X, Y  int
	Text  string
	Steps uint64
}

func (p *Process) textExtent(hdc uint16, n int) (int, int) {
	w, hgt := 8, 16
	if d, ok := p.dc(hdc); ok {
		if obj, ok := p.Objects.Get(d.Font, ObjFont); ok && obj.Font.Height != 0 {
			hgt = obj.Font.Height
			if hgt < 0 {
				hgt = -hgt
			}
			w = hgt / 2
		}
	}
	return w * n, hgt
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
		if animate && idx < len(p.PalMap) {
			p.SysPalette[p.PalMap[idx]] = RGB{r, g, bl}
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
		obj = &Object{Kind: ObjFont, Font: &Font{Height: 16, Width: 8, FaceName: "System"}, Stock: true}
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
