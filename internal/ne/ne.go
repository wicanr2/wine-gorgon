// Package ne 解析 Win16 的 NE（New Executable）映像。
//
// 這是 wine-gorgon 最底下那一層：把一個 16 位元 Windows 執行檔拆成
// 「段、重定位、匯入、資源」四樣東西。上面的載入器拿這些東西鋪記憶體、
// 配 selector、把匯入目標換成可攔截的 thunk。
//
// 為什麼自己寫而不是找套件：NE 的重定位表決定了**哪些位址是 API 呼叫**，
// 而攔截 API 正是這個工具的全部重點。這一層要能逐筆說出「這個 far call
// 打到 GDI 的第幾號」，不能只是「把檔案載進來」。
package ne

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// FormatError 是格式不合的錯誤；讀檔失敗會原樣包回 os 的錯。
type FormatError struct{ Msg string }

func (e *FormatError) Error() string { return "ne: " + e.Msg }

func errf(format string, a ...any) error {
	return &FormatError{Msg: fmt.Sprintf(format, a...)}
}

// 重定位的四種來源（NE 規格的 low 2 bits of flags）。
const (
	RelInternalRef = 0 // 同一個模組內的段
	RelImportOrd   = 1 // 匯入：模組序號 + 函式序號
	RelImportName  = 2 // 匯入：模組序號 + 名稱表位移
	RelOSFixup     = 3 // 給 FP 模擬器用的修補
)

// 重定位的位址型別（flags 的高位以外那個 byte）。
const (
	AddrLoByte  = 0
	AddrSegment = 2
	AddrFarAddr = 3 // 32 位元 far pointer：這是 far call 的形狀
	AddrOffset  = 5
)

// Segment 是 NE 的一個段。
type Segment struct {
	Index      int    // 1-based，NE 自己就是 1-based
	FileSector uint16 // 以 SectorShift 為單位
	Length     uint16 // 0 表示 64 KiB
	Flags      uint16
	MinAlloc   uint16 // 0 表示 64 KiB
	Data       []byte // 已從檔案讀出（Length 為 0 時長度是 0x10000）
	Relocs     []Reloc
}

// Movable 回報這個段有沒有 MOVEABLE 旗標（NE 段旗標 bit4）。
func (s Segment) Movable() bool { return s.Flags&0x0010 != 0 }

// IsData 回報這是不是資料段（bit0）。
func (s Segment) IsData() bool { return s.Flags&0x0001 != 0 }

// Reloc 是一筆重定位。
//
// `Offset` 是**段內第一個要修補的位置**；NE 用鏈結串列表示同一個目標的多處
// 修補——被修補的那個 word 存的是下一處的位移，`0xFFFF` 結束。載入器要自己
// 走那條鏈，這裡不展開（展開需要段的原始內容，而那是載入器的工作）。
type Reloc struct {
	AddrType uint8
	Kind     uint8 // Rel* 常數
	Offset   uint16
	Additive bool // flags bit2：加法式而非鏈結式

	// Kind == RelInternalRef
	TargetSeg uint8
	TargetOff uint16

	// Kind == RelImportOrd / RelImportName
	Module  uint8  // 1-based，index 進 ModuleNames
	Ordinal uint16 // RelImportOrd
	NameOff uint16 // RelImportName：進 imported-name table 的位移
}

// Import 是一筆相異的匯入項（模組 + 序號或名稱）。
type Import struct {
	Module  string
	Ordinal uint16 // 0 表示用名稱
	Name    string
	Refs    int // 全庫有幾處重定位指向它
}

// Key 給人看，也給 map 當鍵：`GDI.#45` 或 `KERNEL.GLOBALALLOC`。
func (i Import) Key() string {
	if i.Name != "" {
		return i.Module + "." + i.Name
	}
	return fmt.Sprintf("%s.#%d", i.Module, i.Ordinal)
}

// Image 是解析完的整個 NE。
type Image struct {
	HeaderOff   uint32
	SectorShift uint16
	CSIP        uint32 // 進入點：高 16 位是段號，低 16 位是位移
	SSSP        uint32
	HeapSize    uint16
	StackSize   uint16
	Flags       uint16

	ModuleNames []string // 1-based 使用；這裡是 0-based 陣列
	Segments    []Segment
	Imports     []Import

	raw          []byte
	impNameTable uint32
}

func u16(b []byte, off uint32) (uint16, error) {
	if int(off)+2 > len(b) {
		return 0, errf("讀 u16 越界：位移 0x%X，檔長 0x%X", off, len(b))
	}
	return binary.LittleEndian.Uint16(b[off:]), nil
}

// pascalString 讀 NE 慣用的「長度前綴字串」。
func pascalString(b []byte, off uint32) (string, error) {
	if int(off) >= len(b) {
		return "", errf("讀字串越界：位移 0x%X", off)
	}
	n := uint32(b[off])
	if int(off+1+n) > len(b) {
		return "", errf("字串長度越界：位移 0x%X 長度 %d", off, n)
	}
	return string(b[off+1 : off+1+n]), nil
}

// Open 讀一個 NE 檔。
func Open(path string) (*Image, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse 解析記憶體裡的 NE 映像。
func Parse(raw []byte) (*Image, error) {
	if len(raw) < 0x40 || raw[0] != 'M' || raw[1] != 'Z' {
		return nil, errf("不是 MZ 檔頭")
	}
	neOff32, err := u16(raw, 0x3C)
	if err != nil {
		return nil, err
	}
	ne := uint32(neOff32)
	if int(ne)+0x40 > len(raw) {
		return nil, errf("NE 檔頭位移 0x%X 超出檔尾", ne)
	}
	if raw[ne] != 'N' || raw[ne+1] != 'E' {
		return nil, errf("位移 0x%X 不是 NE 簽章（讀到 %q）", ne, raw[ne:ne+2])
	}

	img := &Image{HeaderOff: ne, raw: raw}
	get := func(off uint32) uint16 {
		v, e := u16(raw, ne+off)
		if e != nil && err == nil {
			err = e
		}
		return v
	}
	img.Flags = get(0x0C)
	segCount := get(0x1C)
	modRefCount := get(0x1E)
	segTableOff := ne + uint32(get(0x22))
	resTableOff := ne + uint32(get(0x24))
	modRefOff := ne + uint32(get(0x28))
	img.impNameTable = ne + uint32(get(0x2A))
	img.CSIP = uint32(get(0x14)) | uint32(get(0x16))<<16
	img.SSSP = uint32(get(0x18)) | uint32(get(0x1A))<<16
	img.StackSize = get(0x12)
	img.HeapSize = get(0x10)
	img.SectorShift = get(0x32)
	if err != nil {
		return nil, err
	}
	_ = resTableOff // 資源表另一支處理（docs/spec/002 §4）

	// 模組參考表：每筆是一個進 imported-name table 的位移。
	img.ModuleNames = make([]string, 0, modRefCount)
	for i := uint32(0); i < uint32(modRefCount); i++ {
		off, e := u16(raw, modRefOff+2*i)
		if e != nil {
			return nil, e
		}
		name, e := pascalString(raw, img.impNameTable+uint32(off))
		if e != nil {
			return nil, e
		}
		img.ModuleNames = append(img.ModuleNames, name)
	}

	// 段表 + 每段的重定位。
	seen := map[string]int{} // Import.Key() → Imports 的索引
	for i := uint32(0); i < uint32(segCount); i++ {
		e := segTableOff + 8*i
		if int(e)+8 > len(raw) {
			return nil, errf("段表第 %d 筆越界", i+1)
		}
		s := Segment{
			Index:      int(i) + 1,
			FileSector: binary.LittleEndian.Uint16(raw[e:]),
			Length:     binary.LittleEndian.Uint16(raw[e+2:]),
			Flags:      binary.LittleEndian.Uint16(raw[e+4:]),
			MinAlloc:   binary.LittleEndian.Uint16(raw[e+6:]),
		}
		size := uint32(s.Length)
		if s.Length == 0 && s.FileSector != 0 {
			size = 0x10000
		}
		if s.FileSector != 0 {
			base := uint32(s.FileSector) << img.SectorShift
			if int(base+size) > len(raw) {
				return nil, errf("段 %d 的內容越界（0x%X + 0x%X）", s.Index, base, size)
			}
			s.Data = raw[base : base+size]

			// 段旗標 bit8 ＝ 後面接一張重定位表。
			if s.Flags&0x0100 != 0 {
				relOff := base + size
				n, e := u16(raw, relOff)
				if e != nil {
					return nil, e
				}
				for k := uint32(0); k < uint32(n); k++ {
					r := relOff + 2 + 8*k
					if int(r)+8 > len(raw) {
						return nil, errf("段 %d 的重定位第 %d 筆越界", s.Index, k)
					}
					rec := Reloc{
						AddrType: raw[r],
						Kind:     raw[r+1] & 3,
						Additive: raw[r+1]&4 != 0,
						Offset:   binary.LittleEndian.Uint16(raw[r+2:]),
					}
					t1 := binary.LittleEndian.Uint16(raw[r+4:])
					t2 := binary.LittleEndian.Uint16(raw[r+6:])
					switch rec.Kind {
					case RelInternalRef:
						rec.TargetSeg = uint8(t1)
						rec.TargetOff = t2
					case RelImportOrd:
						rec.Module, rec.Ordinal = uint8(t1), t2
					case RelImportName:
						rec.Module, rec.NameOff = uint8(t1), t2
					}
					s.Relocs = append(s.Relocs, rec)

					if rec.Kind == RelImportOrd || rec.Kind == RelImportName {
						imp, e := img.importOf(rec)
						if e != nil {
							return nil, e
						}
						key := imp.Key()
						if idx, ok := seen[key]; ok {
							img.Imports[idx].Refs++
						} else {
							imp.Refs = 1
							seen[key] = len(img.Imports)
							img.Imports = append(img.Imports, imp)
						}
					}
				}
			}
		}
		img.Segments = append(img.Segments, s)
	}
	return img, nil
}

func (img *Image) importOf(r Reloc) (Import, error) {
	if int(r.Module) < 1 || int(r.Module) > len(img.ModuleNames) {
		return Import{}, errf("模組參考 %d 超出範圍（共 %d 個）", r.Module, len(img.ModuleNames))
	}
	imp := Import{Module: img.ModuleNames[r.Module-1]}
	if r.Kind == RelImportOrd {
		imp.Ordinal = r.Ordinal
		return imp, nil
	}
	name, err := pascalString(img.raw, img.impNameTable+uint32(r.NameOff))
	if err != nil {
		return Import{}, err
	}
	imp.Name = name
	return imp, nil
}

// ImportsByModule 依模組把匯入分組，方便盤點 API 表面。
func (img *Image) ImportsByModule() map[string][]Import {
	out := map[string][]Import{}
	for _, imp := range img.Imports {
		out[imp.Module] = append(out[imp.Module], imp)
	}
	return out
}

// ErrNoEntry 是進入點段號為 0（NE 允許，表示沒有進入點）時回的錯。
var ErrNoEntry = errors.New("ne: 這個映像沒有進入點")

// Entry 回傳進入點的 (段號, 位移)。段號是 1-based。
func (img *Image) Entry() (seg int, off uint16, err error) {
	seg = int(img.CSIP >> 16)
	if seg == 0 {
		return 0, 0, ErrNoEntry
	}
	return seg, uint16(img.CSIP), nil
}
