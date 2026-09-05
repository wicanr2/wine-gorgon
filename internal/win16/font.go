package win16

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wicanr2/wine-gorgon/internal/ne"
)

// Windows 2.x／3.x 的點陣字型（FNT）。
//
// 一個 `.FON` 檔本身就是一個 NE，字型放在 `RT_FONT` 資源裡；一個檔可以
// 裝很多個字面。CIV.EXE 用 `AddFontResource` 載入自己的 `CIVFONTS.FON`，
// 所以**畫面上的每一個字都來自那個檔**——沒有它就沒有逐點相同可言。
//
// 字形的位元圖是**直行排列**的：寬 W 的字被切成 ceil(W/8) 個「八像素寬
// 的直條」，每一條連續放 H 個 byte（每個 byte 是那一列的八個像素，
// 最高位在左）。這和一般由左到右、由上到下的排法不同，照後者讀會得到
// 一團看起來像雜訊但長度正確的資料。

// BitmapFont 是一個字面。
type BitmapFont struct {
	Face       string
	Height     int // dfPixHeight
	Ascent     int
	IntLeading int
	ExtLeading int
	Points     int
	Weight     int
	Italic     bool
	AvgWidth   int
	MaxWidth   int
	FirstChar  byte
	LastChar   byte
	DefChar    byte
	BreakChar  byte

	widths  []int    // 每個字的像素寬（索引 ＝ 字碼 − FirstChar）
	glyphs  [][]byte // 每個字的位元組（直行排列）
	version uint16
}

// CharWidth 回一個字的像素寬；沒有這個字就用預設字。
func (f *BitmapFont) CharWidth(c byte) int {
	i := f.index(c)
	if i < 0 {
		return 0
	}
	return f.widths[i]
}

// TextWidth 回一串字的像素寬。
func (f *BitmapFont) TextWidth(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		n += f.CharWidth(s[i])
	}
	return n
}

func (f *BitmapFont) index(c byte) int {
	if c < f.FirstChar || c > f.LastChar {
		c = f.DefChar
		if c < f.FirstChar || c > f.LastChar {
			return -1
		}
	}
	return int(c - f.FirstChar)
}

// Pixel 回報字 c 的 (x,y) 是不是前景。
func (f *BitmapFont) Pixel(c byte, x, y int) bool {
	i := f.index(c)
	if i < 0 || y < 0 || y >= f.Height || x < 0 || x >= f.widths[i] {
		return false
	}
	g := f.glyphs[i]
	// 直行排列：第 x/8 條、該條的第 y 個 byte、byte 內的第 x%8 位（高位在左）。
	off := (x/8)*f.Height + y
	if off >= len(g) {
		return false
	}
	return g[off]&(0x80>>(x%8)) != 0
}

// ParseFNT 解一份 FNT 資源。
func ParseFNT(b []byte) (*BitmapFont, error) {
	if len(b) < 0x76 {
		return nil, fmt.Errorf("win16: FNT 只有 %d bytes，連檔頭都不夠", len(b))
	}
	u16 := func(o int) uint16 { return binary.LittleEndian.Uint16(b[o:]) }
	u32 := func(o int) uint32 { return binary.LittleEndian.Uint32(b[o:]) }

	f := &BitmapFont{
		version:    u16(0x00),
		Points:     int(u16(0x44)),
		Ascent:     int(u16(0x4A)),
		IntLeading: int(u16(0x4C)),
		ExtLeading: int(u16(0x4E)),
		Italic:     b[0x50] != 0,
		Weight:     int(u16(0x53)),
		Height:     int(u16(0x58)),
		AvgWidth:   int(u16(0x5B)),
		MaxWidth:   int(u16(0x5D)),
		FirstChar:  b[0x5F],
		LastChar:   b[0x60],
		DefChar:    b[0x61] + b[0x5F],
		BreakChar:  b[0x62] + b[0x5F],
	}
	if f.version != 0x0200 && f.version != 0x0300 {
		return nil, fmt.Errorf("win16: 不認得的 FNT 版本 %04X", f.version)
	}
	if f.LastChar < f.FirstChar {
		return nil, fmt.Errorf("win16: FNT 的字碼範圍是 %d..%d", f.FirstChar, f.LastChar)
	}
	if off := int(u32(0x69)); off > 0 && off < len(b) {
		f.Face = cstr(b[off:])
	}

	// 字表：2.0 是「寬(2) ＋ 位移(2)」，3.0 是「寬(2) ＋ 位移(4)」。
	// 表尾多一項（哨兵），這裡不需要。
	entrySize, tableOff := 4, 0x76
	if f.version == 0x0300 {
		entrySize, tableOff = 6, 0x94
	}
	n := int(f.LastChar) - int(f.FirstChar) + 1
	f.widths = make([]int, n)
	f.glyphs = make([][]byte, n)
	for i := 0; i < n; i++ {
		e := tableOff + i*entrySize
		if e+entrySize > len(b) {
			return nil, fmt.Errorf("win16: FNT 字表第 %d 項超出資源尾端", i)
		}
		w := int(u16(e))
		var off int
		if f.version == 0x0300 {
			off = int(u32(e + 2))
		} else {
			off = int(u16(e + 2))
		}
		f.widths[i] = w
		size := ((w + 7) / 8) * f.Height
		if off <= 0 || off+size > len(b) {
			f.glyphs[i] = make([]byte, size) // 缺資料就當成空白，不要整份放棄
			continue
		}
		f.glyphs[i] = b[off : off+size]
	}
	return f, nil
}

func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
		if i > 64 {
			break
		}
	}
	return ""
}

// LoadFontFile 把一個 `.FON`（本身是 NE）裡的所有字面讀進來。
func (p *Process) LoadFontFile(dosPath string) (int, error) {
	h, err := p.FS.Open(dosPath, 0)
	if err != nil {
		return 0, err
	}
	f, _ := p.FS.File(h)
	defer p.FS.Close(h)

	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	raw := make([]byte, st.Size())
	if _, err := f.Read(raw); err != nil {
		return 0, err
	}
	return p.loadFontImage(raw)
}

func (p *Process) loadFontImage(raw []byte) (int, error) {
	img, err := ne.Parse(raw)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range img.Resources {
		if r.TypeID != ne.RTFont || r.TypeName != "" {
			continue
		}
		data, err := img.ResourceData(r)
		if err != nil {
			continue
		}
		fnt, err := ParseFNT(data)
		if err != nil {
			p.note("字型資源 %s 解不開：%v", r.String(), err)
			continue
		}
		p.Fonts = append(p.Fonts, fnt)
		n++
	}
	return n, nil
}

// LoadInstalledFonts 把資料目錄裡所有的 `.FON` 載進來。
//
// 真 Windows 是從 `WIN.INI` 的 `[fonts]` 段載入安裝過的字型；那一段是
// 安裝程式寫的，而我們沒有跑安裝程式。這裡直接把遊戲目錄裡的字型當成
// 「已安裝」——CIV.EXE 的 `CIVFONTS.FON` 就是它自己的安裝程式登記進去的。
func (p *Process) LoadInstalledFonts() (int, error) {
	if p.FS == nil || p.FS.Root == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(p.FS.Root)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".fon") {
			continue
		}
		n, err := p.LoadFontFile(e.Name())
		if err != nil {
			p.note("載入字型檔 %s 失敗：%v", e.Name(), err)
			continue
		}
		p.FontFiles = append(p.FontFiles, e.Name())
		total += n
	}
	return total, nil
}
