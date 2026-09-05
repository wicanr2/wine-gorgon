package win16

import "fmt"

// GDI 這一層畫的是**8 位元索引色**的畫布。
//
// 原版 Civilization 跑在 256 色的 Windows 上，畫面上的每一個像素就是一個
// 調色盤索引；逐點比對也是在索引上比，不是在 RGB 上比。把索引當成第一
// 級的資料（而不是先轉成 RGB 再畫）可以讓「和原版逐點相同」這件事有
// 明確的意義。

// Surface 是一塊 8 位元索引色的畫布。
type Surface struct {
	W, H   int
	Stride int // 每列的位元組數；GDI 的 DDB 是**字組對齊**
	Bits   []byte
}

// NewSurface 造一塊指定大小的畫布，內容全零。
func NewSurface(w, h int) *Surface {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	stride := (w + 1) &^ 1 // 8bpp 的字組對齊就是「補到偶數」
	return &Surface{W: w, H: h, Stride: stride, Bits: make([]byte, stride*h)}
}

// At 讀一個像素；超出範圍回 0。
func (s *Surface) At(x, y int) byte {
	if x < 0 || y < 0 || x >= s.W || y >= s.H {
		return 0
	}
	return s.Bits[y*s.Stride+x]
}

// Set 寫一個像素；超出範圍就丟掉（GDI 的裁切語意）。
func (s *Surface) Set(x, y int, v byte) {
	if x < 0 || y < 0 || x >= s.W || y >= s.H {
		return
	}
	s.Bits[y*s.Stride+x] = v
}

// RGB 是調色盤的一格。
type RGB struct{ R, G, B uint8 }

// ObjKind 是 GDI 物件的種類。
type ObjKind int

// GDI 物件種類。
const (
	ObjBitmap ObjKind = iota
	ObjBrush
	ObjPen
	ObjFont
	ObjPalette
	ObjDC
)

func (k ObjKind) String() string {
	switch k {
	case ObjBitmap:
		return "點陣圖"
	case ObjBrush:
		return "筆刷"
	case ObjPen:
		return "畫筆"
	case ObjFont:
		return "字型"
	case ObjPalette:
		return "調色盤"
	default:
		return "DC"
	}
}

// Bitmap 是一張 DDB。
type Bitmap struct {
	Surf   *Surface
	Planes uint8
	BPP    uint8
	DimX   int // SetBitmapDimension 記下的邏輯尺寸，GDI 自己不用
	DimY   int
}

// Brush 是實心筆刷或圖樣筆刷。
type Brush struct {
	Color  uint32 // COLORREF
	Index  byte   // 對映到調色盤索引之後的值
	Hollow bool
	Patt   uint16 // 圖樣筆刷的點陣圖 handle；0 表示實心
}

// Pen 是畫筆。
type Pen struct {
	Style int
	Width int
	Color uint32
	Index byte
}

// Font 是一份 LOGFONT 的要求，外加它實際對到的點陣字面。
type Font struct {
	Height   int
	Width    int
	Weight   int
	Italic   bool
	FaceName string

	// Bitmap 是實際挑中的點陣字面；沒有載入任何字型時是 nil。
	Bitmap *BitmapFont
}

// Palette 是一份邏輯調色盤。
//
// `Map` 是 RealizePalette 之後「邏輯索引 → 實體索引」的對應，存在調色盤
// 自己身上而不是行程上——同時存在多份邏輯調色盤時，AnimatePalette 動的
// 必須是**它自己那一份**的實體格子。
type Palette struct {
	Entries []RGB
	Flags   []uint8
	Map     []byte
}

// Object 是 handle 表裡的一格。
type Object struct {
	Handle  uint16
	Kind    ObjKind
	Bitmap  *Bitmap
	Brush   *Brush
	Pen     *Pen
	Font    *Font
	Palette *Palette
	DC      *DC
	Stock   bool // 系統物件不能被 DeleteObject 刪掉
}

// Objects 是 GDI 與 USER 共用的 handle 表。
//
// handle 從 0x1000 起發，刻意和 selector（段的是 sel&7==7、動態的從
// 0x8007 起）分開——除錯時看到一個數字就知道它是哪一種東西。
type Objects struct {
	m    map[uint16]*Object
	next uint16
}

// NewObjects 造一份空的 handle 表。
func NewObjects() *Objects { return &Objects{m: map[uint16]*Object{}, next: 0x1000} }

// Add 放進一個物件並回傳 handle。
func (o *Objects) Add(obj *Object) uint16 {
	h := o.next
	o.next++
	if o.next == 0 {
		o.next = 0x1000
	}
	obj.Handle = h
	o.m[h] = obj
	return h
}

// Get 取一個物件；種類不符也算找不到。
func (o *Objects) Get(h uint16, kind ObjKind) (*Object, bool) {
	obj, ok := o.m[h]
	if !ok || obj.Kind != kind {
		return nil, false
	}
	return obj, true
}

// Any 不管種類取一個物件。
func (o *Objects) Any(h uint16) (*Object, bool) {
	obj, ok := o.m[h]
	return obj, ok
}

// Delete 刪一個物件；系統物件刪不掉。
func (o *Objects) Delete(h uint16) bool {
	obj, ok := o.m[h]
	if !ok || obj.Stock {
		return false
	}
	delete(o.m, h)
	return true
}

// Count 回報目前有幾個物件，用來看有沒有洩漏。
func (o *Objects) Count() int { return len(o.m) }

func (o *Objects) String() string { return fmt.Sprintf("Objects(%d)", len(o.m)) }
