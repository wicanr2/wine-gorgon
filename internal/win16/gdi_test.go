package win16

import "testing"

func TestROPTableCoversTheStandardCodes(t *testing.T) {
	// ROP 碼的第 16..23 位就是真值表。這幾個是最常用的，
	// 如果表的解讀方向反了，SRCCOPY 會變成 DSTCOPY——而畫面看起來
	// 只是「沒更新」，不會報錯。
	cases := []struct {
		name          string
		rop           uint32
		p, s, d, want byte
	}{
		{"SRCCOPY", 0x00CC0020, 0x00, 0x5A, 0xA5, 0x5A},
		{"SRCAND", 0x008800C6, 0x00, 0x5A, 0xF0, 0x50},
		{"SRCPAINT", 0x00EE0086, 0x00, 0x5A, 0xF0, 0xFA},
		{"SRCINVERT", 0x00660046, 0x00, 0x5A, 0xF0, 0xAA},
		{"BLACKNESS", 0x00000042, 0xFF, 0xFF, 0xFF, 0x00},
		{"WHITENESS", 0x00FF0062, 0x00, 0x00, 0x00, 0xFF},
		{"DSTINVERT", 0x00550009, 0x00, 0x00, 0x5A, 0xA5},
		{"PATCOPY", 0x00F00021, 0x3C, 0x00, 0xFF, 0x3C},
		{"NOTSRCCOPY", 0x00330008, 0x00, 0x5A, 0x00, 0xA5},
	}
	for _, tc := range cases {
		got := rop3(uint8(tc.rop>>16), tc.p, tc.s, tc.d)
		if got != tc.want {
			t.Errorf("%s：P=%02X S=%02X D=%02X → %02X，預期 %02X",
				tc.name, tc.p, tc.s, tc.d, got, tc.want)
		}
	}
}

// 同一塊畫布上往右下搬要**倒著搬**，否則來源會被自己蓋掉。
// 這是捲動地圖時會踩到的。
func TestBitBltOverlapMovesRight(t *testing.T) {
	surf := NewSurface(8, 1)
	for i := 0; i < 8; i++ {
		surf.Set(i, 0, byte(i+1))
	}
	dc := &DC{Surf: surf, ClipR: 8, ClipB: 1}
	BitBlt(dc, 2, 0, 6, 1, dc, 0, 0, 0x00CC0020, 0)
	want := []byte{1, 2, 1, 2, 3, 4, 5, 6}
	for i, w := range want {
		if got := surf.At(i, 0); got != w {
			t.Fatalf("第 %d 個像素 = %d，預期 %d（整列 %v）", i, got, w, surf.Bits[:8])
		}
	}
}

func TestBitBltClipsToDestination(t *testing.T) {
	dst := NewSurface(4, 4)
	src := NewSurface(4, 4)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.Set(x, y, 9)
		}
	}
	// 目的地 DC 只准畫左上 2×2。
	d := &DC{Surf: dst, ClipR: 2, ClipB: 2}
	s := &DC{Surf: src, ClipR: 4, ClipB: 4}
	BitBlt(d, 0, 0, 4, 4, s, 0, 0, 0x00CC0020, 0)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			want := byte(0)
			if x < 2 && y < 2 {
				want = 9
			}
			if got := dst.At(x, y); got != want {
				t.Errorf("(%d,%d) = %d，預期 %d", x, y, got, want)
			}
		}
	}
}

// 裁切之後來源不能跟著錯位：這是「畫出來的東西整體偏移」那一類 bug。
func TestBitBltClipKeepsSourceAligned(t *testing.T) {
	dst := NewSurface(8, 1)
	src := NewSurface(8, 1)
	for i := 0; i < 8; i++ {
		src.Set(i, 0, byte(i+1))
	}
	d := &DC{Surf: dst, ClipL: 3, ClipR: 8, ClipB: 1}
	s := &DC{Surf: src, ClipR: 8, ClipB: 1}
	BitBlt(d, 0, 0, 8, 1, s, 0, 0, 0x00CC0020, 0)
	// 目的地的 x=3 對應來源的 x=3，而不是來源的 x=0。
	if got := dst.At(3, 0); got != 4 {
		t.Errorf("dst[3] = %d，預期 4（來源沒對齊就會是 1）", got)
	}
}

func TestRealizePaletteStartsAtTen(t *testing.T) {
	p := &Process{}
	p.initPalette()
	pal := &Palette{Entries: []RGB{
		{1, 2, 3},
		{0, 0, 0},       // 和靜態色 0 一樣 → 直接用索引 0
		{255, 255, 255}, // 和靜態色 255 一樣 → 索引 255
		{4, 5, 6},
	}}
	if n := p.realizePalette(pal); n != 2 {
		t.Errorf("換了 %d 格，預期 2", n)
	}
	want := []byte{10, 0, 255, 11}
	for i, w := range want {
		if p.PalMap[i] != w {
			t.Errorf("邏輯索引 %d → 實體 %d，預期 %d", i, p.PalMap[i], w)
		}
	}
	if p.SysPalette[10] != (RGB{1, 2, 3}) || p.SysPalette[11] != (RGB{4, 5, 6}) {
		t.Errorf("實體調色盤沒填對：%v %v", p.SysPalette[10], p.SysPalette[11])
	}
}

func TestColorIndexFindsNearest(t *testing.T) {
	p := &Process{}
	p.initPalette()
	// 0x00BBGGRR：純紅在靜態色的後十格（索引 249）。
	if got := p.colorIndex(0x000000FF); got != 249 {
		t.Errorf("純紅 → %d，預期 249", got)
	}
	if got := p.colorIndex(0x00C0C0C0); got != 7 {
		t.Errorf("淺灰 → %d，預期 7", got)
	}
}
