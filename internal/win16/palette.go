package win16

// Windows 的 256 色系統調色盤前十格與後十格是**靜態色**，應用程式動不了；
// 中間的 236 格才是給邏輯調色盤用的。
//
// 這件事在 civ1 專案那邊獨立量到過：它的圖集索引要加 `0x0A` 才是實體
// 調色盤索引（`docs/re/315` 一族）。0x0A ＝ 10 ＝ 前十格靜態色——
// 兩邊對得起來，所以這個版面不是猜的。
var staticPalette = [20]RGB{
	{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
	{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
	{192, 220, 192}, {166, 202, 240},
	{255, 251, 240}, {160, 160, 164}, {128, 128, 128}, {255, 0, 0},
	{0, 255, 0}, {255, 255, 0}, {0, 0, 255}, {255, 0, 255},
	{0, 255, 255}, {255, 255, 255},
}

// FirstFreePaletteIndex 是邏輯調色盤第一個可以落腳的實體索引。
const FirstFreePaletteIndex = 10

// initPalette 把靜態色放進實體調色盤。
func (p *Process) initPalette() {
	for i := 0; i < 10; i++ {
		p.SysPalette[i] = staticPalette[i]
		p.SysPalette[246+i] = staticPalette[10+i]
	}
}

// realizePalette 把一份邏輯調色盤搬進實體調色盤，回傳換了幾格。
//
// Windows 的規則是「能對上靜態色就用靜態色，否則從第一個空格往後填」。
// 這裡照做，因為**第一幀的每一個索引都由這個對應決定**。
func (p *Process) realizePalette(pal *Palette) int {
	changed := 0
	next := FirstFreePaletteIndex
	p.PalMap = make([]byte, len(pal.Entries))
	for i, e := range pal.Entries {
		if idx, ok := staticIndex(e); ok {
			p.PalMap[i] = byte(idx)
			continue
		}
		if next > 245 {
			p.note("邏輯調色盤有 %d 格，實體調色盤（10..245）放不下", len(pal.Entries))
			p.PalMap[i] = byte(245)
			continue
		}
		if p.SysPalette[next] != e {
			p.SysPalette[next] = e
			changed++
		}
		p.PalMap[i] = byte(next)
		next++
	}
	return changed
}

func staticIndex(e RGB) (int, bool) {
	for i := 0; i < 10; i++ {
		if staticPalette[i] == e {
			return i, true
		}
		if staticPalette[10+i] == e {
			return 246 + i, true
		}
	}
	return 0, false
}

// colorIndex 把一個 COLORREF 對到最接近的實體調色盤索引。
//
// COLORREF 的高位元組是旗標：`0x01`＝PALETTEINDEX（低位就是索引），
// `0x02`＝PALETTERGB（照 RGB 找最近的）。GDI 對實心筆刷與畫筆做的就是
// 這件事，用的是**平方歐氏距離**——和 civ1 量到的 `sub_34EE1` 同一種。
func (p *Process) colorIndex(c uint32) byte {
	switch byte(c >> 24) {
	case 0x01:
		i := c & 0xFFFF
		if int(i) < len(p.PalMap) {
			return p.PalMap[i]
		}
		return byte(i)
	}
	want := RGB{uint8(c), uint8(c >> 8), uint8(c >> 16)}
	best, bestD := 0, 1<<30
	for i, e := range p.SysPalette {
		dr := int(e.R) - int(want.R)
		dg := int(e.G) - int(want.G)
		db := int(e.B) - int(want.B)
		d := dr*dr + dg*dg + db*db
		if d < bestD {
			best, bestD = i, d
			if d == 0 {
				break
			}
		}
	}
	return byte(best)
}
