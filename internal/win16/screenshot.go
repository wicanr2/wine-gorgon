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
func (p *Process) Image(s *Surface) *image.Paletted {
	pal := make(color.Palette, 256)
	for i, e := range p.SysPalette {
		pal[i] = color.RGBA{e.R, e.G, e.B, 255}
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
