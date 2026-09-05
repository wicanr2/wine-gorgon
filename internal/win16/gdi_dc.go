package win16

// DC 是一個裝置內容。
//
// 它指向一塊 Surface，外加一個原點與裁切矩形。視窗 DC 的原點是那個視窗
// 客戶區的左上角，裁切矩形是客戶區；記憶體 DC 的原點是 (0,0)、裁切矩形
// 是整張點陣圖。**兩者共用同一條路徑**——BitBlt 不必分「畫到螢幕」和
// 「畫到點陣圖」兩種寫法。
type DC struct {
	Handle uint16
	Surf   *Surface

	// OrgX／OrgY 是這個 DC 的 (0,0) 在 Surf 上的位置。
	OrgX, OrgY int
	// 裁切矩形，Surf 座標系，右下不含。
	ClipL, ClipT, ClipR, ClipB int

	Window uint16 // 屬於哪個視窗；0 表示記憶體 DC
	// ClipRel 是**相對客戶區**的額外裁剪（BeginPaint 的更新矩形）。
	// 存相對值而不是絕對值，視窗一移動就能重算——見 refreshWindowDC。
	ClipRel *[4]int
	Bitmap  uint16 // 選進來的點陣圖
	Brush   uint16
	Pen     uint16
	Font    uint16
	Pal     uint16

	TextColor uint32
	BkColor   uint32
	BkMode    int
	TextAlign uint16
	PolyFill  int

	CurX, CurY int
}

// clipTo 把一個 DC 座標的矩形換算成 Surf 座標並裁切。
func (d *DC) clipTo(x, y, w, h int) (sx, sy, sw, sh int) {
	l, t := x+d.OrgX, y+d.OrgY
	r, b := l+w, t+h
	if l < d.ClipL {
		l = d.ClipL
	}
	if t < d.ClipT {
		t = d.ClipT
	}
	if r > d.ClipR {
		r = d.ClipR
	}
	if b > d.ClipB {
		b = d.ClipB
	}
	if r <= l || b <= t {
		return 0, 0, 0, 0
	}
	return l, t, r - l, b - t
}

// Pixel 讀一個 DC 座標的像素。
func (d *DC) Pixel(x, y int) byte { return d.Surf.At(x+d.OrgX, y+d.OrgY) }

// SetPixel 寫一個 DC 座標的像素（含裁切）。
func (d *DC) SetPixel(x, y int, v byte) {
	sx, sy := x+d.OrgX, y+d.OrgY
	if sx < d.ClipL || sy < d.ClipT || sx >= d.ClipR || sy >= d.ClipB {
		return
	}
	d.Surf.Set(sx, sy, v)
}

// FillRect 用一個索引填滿矩形。
func (d *DC) FillRect(x, y, w, h int, v byte) {
	sx, sy, sw, sh := d.clipTo(x, y, w, h)
	for j := 0; j < sh; j++ {
		row := (sy + j) * d.Surf.Stride
		for i := 0; i < sw; i++ {
			d.Surf.Bits[row+sx+i] = v
		}
	}
}

// rop3 是三元光柵運算的真值表索引。
//
// GDI 的 32 位元 ROP 碼裡，第 16..23 位就是那張真值表：對每一個位元平面，
// 結果 ＝ `(表 >> ((P<<2)|(S<<1)|D)) & 1`。照這個做，256 種 ROP 一次到齊，
// 不必一個個特判 SRCCOPY／SRCAND／SRCINVERT。
func rop3(table uint8, p, s, d byte) byte {
	var out byte
	for bit := 0; bit < 8; bit++ {
		pb := (p >> bit) & 1
		sb := (s >> bit) & 1
		db := (d >> bit) & 1
		if table>>((pb<<2)|(sb<<1)|db)&1 != 0 {
			out |= 1 << bit
		}
	}
	return out
}

// BitBlt 把 src 的一塊搬到 dst，套用 ROP。
//
// 重疊搬移要選對方向，否則同一塊 Surface 上的捲動會把資料抹掉。
func BitBlt(dst *DC, dx, dy, w, h int, src *DC, sx, sy int, rop uint32, pattern byte) {
	if w <= 0 || h <= 0 {
		return
	}
	table := uint8(rop >> 16)

	// 先把目的地裁切，再把同樣的位移套回來源——這樣裁切不會讓來源錯位。
	dl, dt, dw, dh := dst.clipTo(dx, dy, w, h)
	if dw == 0 || dh == 0 {
		return
	}
	shiftX := dl - (dx + dst.OrgX)
	shiftY := dt - (dy + dst.OrgY)
	srcX := sx + src.OrgX + shiftX
	srcY := sy + src.OrgY + shiftY

	sameSurface := src != nil && src.Surf == dst.Surf
	stepY, y0 := 1, 0
	stepX, x0 := 1, 0
	if sameSurface {
		if srcY < dt {
			stepY, y0 = -1, dh-1
		}
		if srcX < dl {
			stepX, x0 = -1, dw-1
		}
	}

	for jj := 0; jj < dh; jj++ {
		j := y0 + jj*stepY
		for ii := 0; ii < dw; ii++ {
			i := x0 + ii*stepX
			var s byte
			if src != nil {
				s = src.Surf.At(srcX+i, srcY+j)
			}
			d := dst.Surf.At(dl+i, dt+j)
			dst.Surf.Set(dl+i, dt+j, rop3(table, pattern, s, d))
		}
	}
}
