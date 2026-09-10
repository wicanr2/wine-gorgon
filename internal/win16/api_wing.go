package win16

// WinG（`WING.DLL` 1.0）——PTO2 把整個畫面畫進一塊 WinG DIB，
// 所以這一組是「畫面變成可定址記憶體」的入口（見 pto2-remake 的
// `docs/spec/14-oracle-wine-gorgon.md` §6）。
//
// 六支的分工：
//
//	#1001 WinGCreateDC            造一個給 WinG 用的記憶體 DC
//	#1002 WinGRecommendDIBFormat  回報「這台機器最快的 DIB 格式」
//	#1003 WinGCreateBitmap        依 header 造 DIB，並把 bits 的 far 指標寫回呼叫端
//	#1004 WinGGetDIBPointer       再問一次 bits 的 far 指標
//	#1006 WinGSetDIBColorTable    設調色盤
//	#1010 WinGBitBlt              把 WinG DIB 搬到目的 DC
//
// WinG 的重點是**呼叫端直接寫那塊 bits**，不經過 GDI。所以 bits 必須真的
// 落在遊戲的位址空間裡，這裡用全域配置取得一塊記憶體，再讓 Surface 直接
// 指向它——兩邊看到的是同一份 bytes，遊戲寫完不必通知我們。

// RegisterWinG 把 WING 的處理器登記上去。
func RegisterWinG(p *Process) {
	h := p.Handlers

	// WinGCreateDC()：造一個記憶體 DC。WinG 的 DC 與一般記憶體 DC 沒有差別，
	// 差別在之後選進去的點陣圖是 DIB 而不是 DDB。
	h["WING.#1001"] = func(p *Process, _ Args) (uint32, error) {
		d := &DC{Surf: NewSurface(1, 1), BkMode: 2}
		d.ClipR, d.ClipB = 1, 1
		d.Font = p.stockObject(13)
		hdc := p.Objects.Add(&Object{Kind: ObjDC, DC: d})
		d.Handle = hdc
		return uint32(hdc), nil
	}

	// WinGRecommendDIBFormat(BITMAPINFO far *pHeader)：回報建議格式。
	//
	// 真 WinG 會依顯示卡回報「哪一種 DIB 搬得最快」。這裡固定回 8bpp、
	// top-down（`biHeight` 為負）——PTO2 的 640×416 畫面就是 8bpp
	// （`RE:seg11:0x8ca4`）。`biWidth=1`／`biHeight=-1` 是 WinG 文件的慣例：
	// 只有正負號與位元深度有意義，實際尺寸由呼叫端在 CreateBitmap 時填。
	h["WING.#1002"] = func(p *Process, a Args) (uint32, error) {
		sel, off := a.Ptr(0)
		put16 := func(d, v uint16) { _ = p.Mod.Mem.WriteU16(sel, off+d, v) }
		put32 := func(d uint16, v uint32) {
			put16(d, uint16(v))
			put16(d+2, uint16(v>>16))
		}
		put32(0, 40)         // biSize
		put32(4, 1)          // biWidth
		put32(8, ^uint32(0)) // biHeight = -1（top-down）
		put16(12, 1)         // biPlanes
		put16(14, 8)         // biBitCount
		put32(16, 0)         // biCompression = BI_RGB
		put32(20, 0)         // biSizeImage
		put32(24, 0)         // biXPelsPerMeter
		put32(28, 0)         // biYPelsPerMeter
		put32(32, 0)         // biClrUsed
		put32(36, 0)         // biClrImportant
		return 1, nil
	}

	// WinGCreateBitmap(HDC, BITMAPINFO far *pHeader, void far * far *ppBits)
	//
	// 這一支是整條路的關鍵：它配一塊記憶體當 DIB 的 bits，把 far 指標寫回
	// 呼叫端，之後遊戲**直接寫那塊記憶體**畫畫面，不再經過 GDI。
	// 所以 Surface 直接指向同一份 bytes——遊戲寫完不必通知我們，
	// 對拍時讀 Surface 就是讀原版當下的畫面。
	h["WING.#1003"] = func(p *Process, a Args) (uint32, error) {
		hsel, hoff := a.Ptr(2)
		rd32 := func(d uint16) uint32 {
			lo, _ := p.Mod.Mem.ReadU16(hsel, hoff+d)
			hi, _ := p.Mod.Mem.ReadU16(hsel, hoff+d+2)
			return uint32(hi)<<16 | uint32(lo)
		}
		w := int(int32(rd32(4)))
		hgt := int(int32(rd32(8)))
		topDown := hgt < 0
		if hgt < 0 {
			hgt = -hgt
		}
		if w <= 0 || hgt <= 0 {
			return 0, nil
		}
		// DIB 每列補齊到 4 bytes（8bpp）。
		stride := (w + 3) &^ 3
		b := p.Mod.Mem.AllocHuge("WinGBitmap", stride*hgt)
		if b == nil {
			p.note("WinGCreateBitmap(%dx%d) 配不到記憶體", w, hgt)
			return 0, nil
		}
		surf := &Surface{W: w, H: hgt, Stride: stride, Bits: b.Data}
		if p.WinGBits == nil {
			p.WinGBits = map[*Surface]uint16{}
		}
		p.WinGBits[surf] = b.Sel
		hbmp := p.Objects.Add(&Object{Kind: ObjBitmap, Bitmap: &Bitmap{
			Surf: surf, Planes: 1, BPP: 8,
		}})
		// 把 bits 的 far 指標寫回 *ppBits。
		psel, poff := a.Ptr(6)
		_ = p.Mod.Mem.WriteU16(psel, poff, 0)
		_ = p.Mod.Mem.WriteU16(psel, poff+2, b.Sel)
		p.note("WinGCreateBitmap %dx%d stride %d top-down=%v → bits %04X:0000",
			w, hgt, stride, topDown, b.Sel)
		return uint32(hbmp), nil
	}

	// WinGGetDIBPointer(HBITMAP, BITMAPINFO far *pHeader)：再問一次 bits 的位址。
	// pHeader 可為 NULL；非 NULL 時要填回實際的尺寸與格式。
	h["WING.#1004"] = func(p *Process, a Args) (uint32, error) {
		o, ok := p.Objects.Get(a.Word(0), ObjBitmap)
		if !ok || o.Bitmap == nil || o.Bitmap.Surf == nil {
			return 0, nil
		}
		if sel, off := a.Ptr(2); sel != 0 {
			s := o.Bitmap.Surf
			put16 := func(d, v uint16) { _ = p.Mod.Mem.WriteU16(sel, off+d, v) }
			put32 := func(d uint16, v uint32) { put16(d, uint16(v)); put16(d+2, uint16(v>>16)) }
			put32(0, 40)
			put32(4, uint32(s.W))
			put32(8, uint32(-int32(s.H))) // top-down
			put16(12, 1)
			put16(14, 8)
			put32(16, 0)
			put32(20, uint32(s.Stride*s.H))
		}
		// bits 的 selector 就是配置時那一塊；Surface 與它共用 bytes。
		return uint32(p.WinGBits[o.Bitmap.Surf]) << 16, nil
	}

	// WinGSetDIBColorTable(HDC, UINT start, UINT num, RGBQUAD far *pColors)
	// 回傳實際設了幾格。調色盤語意與 GDI 的 SetDIBColorTable 相同。
	h["WING.#1006"] = func(p *Process, a Args) (uint32, error) {
		start, num := int(a.Word(2)), int(a.Word(4))
		sel, off := a.Ptr(6)
		n := 0
		for i := 0; i < num && start+i < 256; i++ {
			base := off + uint16(i*4)
			bl, _ := p.Mod.Mem.ReadU8(sel, base)
			gr, _ := p.Mod.Mem.ReadU8(sel, base+1)
			rd, _ := p.Mod.Mem.ReadU8(sel, base+2)
			p.SysPalette[start+i] = RGB{rd, gr, bl}
			n++
		}
		return uint32(n), nil
	}

	// WinGBitBlt(HDC dst, x, y, w, h, HDC src, sx, sy)：把 WinG DIB 搬到目的 DC。
	// 參數順序與 GDI 的 BitBlt 不同（沒有 rop，永遠是 SRCCOPY）。
	h["WING.#1010"] = func(p *Process, a Args) (uint32, error) {
		dst, dok := p.dc(a.Word(0))
		src, sok := p.dc(a.Word(10))
		if !dok || !sok {
			return 0, nil
		}
		x, y := int(int16(a.Word(2))), int(int16(a.Word(4)))
		w, hgt := int(int16(a.Word(6))), int(int16(a.Word(8)))
		sx, sy := int(int16(a.Word(12))), int(int16(a.Word(14)))
		// TODO：實際搬像素還沒接上 GDI 的 blit 路徑。
		// 對拍讀的是 WinG DIB 那塊 bits（遊戲自己寫的），不是這裡的輸出，
		// 所以第一幀對拍不必等這一支；要做視窗畫面才需要。
		p.note("WinGBitBlt dst %04X (%d,%d) %dx%d ← src %04X (%d,%d)：尚未搬像素",
			dst.Handle, x, y, w, hgt, src.Handle, sx, sy)
		return 1, nil
	}
}
