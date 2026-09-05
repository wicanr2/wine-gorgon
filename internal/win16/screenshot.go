package win16

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

// Image 把畫面（或任何一塊 Surface）轉成調色盤圖。
//
// 輸出刻意用 `image.Paletted` 而不是 RGBA：**索引才是原始資料**，
// RGB 是調色盤查出來的。存成調色盤 PNG 之後，逐點比對可以直接比索引，
// 不必擔心色彩轉換這一層引入差異。
// DACBits 是輸出時模擬的 DAC 位元數。
//
// **這不是可有可無的細節。** VGA 的調色盤 DAC 每個通道只有 6 位元：
// 應用程式給的 8 位元值進到硬體時被截成 6 位元，畫面上看到的是
// `(v>>2) * 255 / 63`。原版的參考幀是螢幕擷取，所以它記錄的是**量化之後**
// 的顏色——拿應用程式給的原值去比對，每一個像素都會差一點點，而且看起來
// 像「顏色調錯了」。
//
// 實例：遊戲給的海洋色是 (0,73,128)，參考幀上是 (0,73,130)；
// 草地 (1,128,1) 對 (0,130,0)。兩者都是同一個 6 位元量化的結果。
const DACBits = 6

// dac 把 8 位元通道值量化成 DAC 實際輸出的值。
//
// 展開回 8 位元用的是**位元複製**（`x<<2 | x>>4`）而不是
// `x * 255 / 63`——兩者只差在進位方向，但那一個位元就足以讓每一個像素
// 都判定為不同。實測：遊戲給的草地 (1,128,1) 在參考幀上是 (0,130,0)；
// 位元複製給 130，乘除截斷給 129。
func dac(v uint8) uint8 {
	x := v >> (8 - DACBits)
	return x<<(8-DACBits) | x>>(2*DACBits-8)
}

func (p *Process) Image(s *Surface) *image.Paletted {
	pal := make(color.Palette, 256)
	for i, e := range p.SysPalette {
		pal[i] = color.RGBA{dac(e.R), dac(e.G), dac(e.B), 255}
	}
	img := image.NewPaletted(image.Rect(0, 0, s.W, s.H), pal)
	for y := 0; y < s.H; y++ {
		copy(img.Pix[y*img.Stride:y*img.Stride+s.W], s.Bits[y*s.Stride:y*s.Stride+s.W])
	}
	return img
}

// SavePNG 把一塊 Surface 存成 PNG。
func (p *Process) SavePNG(path string, s *Surface) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, p.Image(s))
}

// SubSurface 取畫面上的一塊，用來只比對主地圖那一區。
func (s *Surface) SubSurface(x, y, w, h int) *Surface {
	out := NewSurface(w, h)
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			out.Set(i, j, s.At(x+i, y+j))
		}
	}
	return out
}

// SaveIndexRaw 把索引原封不動寫成一個檔（每列 W 個 byte，沒有對齊補白）。
// 逐點比對用這個最直接：差一個 byte 就是差一個像素。
func (p *Process) SaveIndexRaw(path string, s *Surface) error {
	buf := make([]byte, 0, s.W*s.H)
	for y := 0; y < s.H; y++ {
		buf = append(buf, s.Bits[y*s.Stride:y*s.Stride+s.W]...)
	}
	return os.WriteFile(path, buf, 0o644)
}
