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
	pal.Map = make([]byte, len(pal.Entries))
	for i, e := range pal.Entries {
		// PC_NOCOLLAPSE（0x04）表示「就算和靜態色一樣也另外給一格」。
		// **這個旗標不能忽略**：收攏一格會讓後面所有的實體索引往前位移，
		// 而遊戲的圖形資料是照「邏輯索引 ＋ 10」寫死的（civ1 `docs/re/315`
		// 一族量到的 `圖集 index ＋ 0x0A`）。位移之後畫面上每一個顏色都錯，
		// 但形狀完全正確——看起來像調色盤壞掉，不像對應錯。
		// **實測：不收攏。** 邏輯索引 i 一律落在實體索引 10+i。
		//
		// 證據有兩份：civ1 專案在原版上量到「圖集索引 ＋ 0x0A ＝ 實體索引」
		// （`docs/re/315` 一族），以及這裡的直接比對——收攏之後灰階多佔一格，
		// 後面所有顏色往前位移一格，畫面上的地形變成紅橙雜訊；不收攏之後
		// 海洋是 (0,73,128)、草地 (1,128,1)、丘陵 (142,103,28)，
		// 和參考幀量到的 (0,73,130)／(0,130,0)／(142,101,28) 逐項吻合
		// （差值來自 6 位元 DAC，見 screenshot.go）。
		//
		// `CollapsePalette` 留著是為了將來遇到真的需要收攏的程式；
		// 預設關閉。
		const pcNoCollapse = 0x04
		noCollapse := !p.CollapsePalette || (i < len(pal.Flags) && pal.Flags[i]&pcNoCollapse != 0)
		if !noCollapse {
			if idx, ok := staticIndex(e); ok {
				pal.Map[i] = byte(idx)
				continue
			}
		}
		if next > 245 {
			p.note("邏輯調色盤有 %d 格，實體調色盤（10..245）放不下", len(pal.Entries))
			pal.Map[i] = byte(245)
			continue
		}
		if p.SysPalette[next] != e {
			p.SysPalette[next] = e
			changed++
		}
		pal.Map[i] = byte(next)
		next++
	}
	p.PalMap = pal.Map
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
