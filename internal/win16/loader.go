package win16

import (
	"fmt"

	"github.com/wicanr2/wine-gorgon/internal/ne"
	"github.com/wicanr2/wine-gorgon/internal/winapi"
)

// ThunkSel 是放 API thunk 的 selector。
//
// 每一個相異匯入在這個段裡佔 4 個 byte（一個 `retf` 加填充），位址就是
// `ThunkSel:index*4`。CPU 執行到這個段就知道「這是一次 API 呼叫」，
// 不必真的解碼那幾個 byte——**攔截點與匯入清單是同一份資料**
// （`docs/spec/001` §4）。
const ThunkSel = 0xF00F

// ThunkStride 是每個 thunk 佔的 byte 數。
const ThunkStride = 4

// Module 是載入好的一個 NE。
type Module struct {
	Image  *ne.Image
	Mem    *Memory
	Thunks []ne.Import // 索引 ＝ thunk 位移 ÷ ThunkStride

	// RelocsApplied 是實際修補過的**位置**數。一筆非加法式重定位可以透過
	// 鏈結串列修補很多處，所以這個數字**可以**大於重定位筆數。
	//
	// CIV.EXE 上它剛好等於筆數：Borland 的 TLINK 一筆只寫一處，鏈結欄一律
	// 是 `0xFFFF`（實測 10,429 條全部如此）。**這不是「鏈沒走」**——早期版本
	// 用「位置數 == 筆數」當成走鏈失敗的警訊，那個啟發式在這支執行檔上誤報。
	RelocsApplied int
	ChainsWalked  int
	ChainLinks    int // 鏈結超過一處的次數；TLINK 產物上是 0
}

// ThunkFor 回傳某個匯入的 thunk 位移；找不到回 (0, false)。
func (m *Module) ThunkFor(key string) (uint16, bool) {
	for i, imp := range m.Thunks {
		if imp.Key() == key {
			return uint16(i * ThunkStride), true
		}
	}
	return 0, false
}

// ImportAt 回答「`ThunkSel:off` 是哪一個 API」。
func (m *Module) ImportAt(off uint16) (ne.Import, bool) {
	i := int(off) / ThunkStride
	if int(off)%ThunkStride != 0 || i < 0 || i >= len(m.Thunks) {
		return ne.Import{}, false
	}
	return m.Thunks[i], true
}

// Load 把映像鋪成位址空間：配段、走重定位鏈、裝 thunk。
func Load(img *ne.Image) (*Module, error) {
	mod := &Module{Image: img, Mem: NewMemory()}

	// 1. 每個段一塊記憶體。
	//
	// 段的配置量取 max(檔案內容, MinAlloc)：資料段的檔案內容通常比它要求的
	// 空間短（後面是 BSS），而程式碼段反過來。取大的那個。
	for _, s := range img.Segments {
		size := len(s.Data)
		alloc := int(s.MinAlloc)
		if s.MinAlloc == 0 {
			alloc = 0x10000
		}
		if alloc > size {
			size = alloc
		}
		if size == 0 {
			size = 1 // 空段也要有 selector，否則 mov es, ax 會炸
		}
		// 自動資料段（DGROUP）後面還要接區域堆疊與區域堆積——它們不在
		// 檔案裡，但 SS:SP 一開始就指到那塊的尾巴。不加就會在第一次 push
		// 越界。
		if s.Index == img.AutoData {
			size += int(img.StackSize) + int(img.HeapSize)
			if size > 0x10000 {
				size = 0x10000
			}
		}
		blk := mod.Mem.Put(SegSelector(s.Index), fmt.Sprintf("seg %d", s.Index), make([]byte, size))
		blk.Fixed = !s.Movable()
		copy(blk.Data, s.Data)
	}

	// 2. thunk 段：每個相異匯入一格。
	mod.Thunks = append(mod.Thunks, img.Imports...)
	thunk := mod.Mem.Put(ThunkSel, "thunk", make([]byte, len(mod.Thunks)*ThunkStride+ThunkStride))
	for i := range mod.Thunks {
		// 0xCB ＝ retf。真正的攔截在 CPU 那一層（看到 CS == ThunkSel 就轉呼叫），
		// 這幾個 byte 只是讓「萬一真的執行到」有定義的行為。
		thunk.Data[i*ThunkStride] = 0xCB
	}

	// 3. 重定位。
	for _, s := range img.Segments {
		sel := SegSelector(s.Index)
		for _, r := range s.Relocs {
			if err := mod.applyReloc(sel, s, r); err != nil {
				return nil, err
			}
		}
	}
	return mod, nil
}

// relocValue 算出一筆重定位要填的 (位移, 段) 值。
func (mod *Module) relocValue(r ne.Reloc) (off uint16, seg uint16, err error) {
	switch r.Kind {
	case ne.RelInternalRef:
		if r.TargetSeg == 0xFF {
			// 0xFF ＝ 走進入點表（movable segment 的間接引用）。
			// 進入點表還沒解，先回一個明顯錯的值而不是靜靜填 0——
			// 靜靜填 0 會在很久以後變成一個看不懂的當機。
			return 0, 0, fmt.Errorf("win16: 尚未支援 INTERNALREF 的 movable 進入點（segment 0xFF）")
		}
		return r.TargetOff, SegSelector(int(r.TargetSeg)), nil
	case ne.RelImportOrd, ne.RelImportName:
		imp, err := mod.Image.ImportForReloc(r)
		if err != nil {
			return 0, 0, err
		}
		// 有些匯入不是函式而是常數（`__AHSHIFT` 被填進 `mov cx, ????`
		// 的立即數，後面接 `shl bx, cl`）。把常數當成函式位址填進去，
		// huge 指標的位移運算會整個歪掉，而且不會當掉。
		if v, ok := winapi.ValueImports[imp.Key()]; ok {
			return v, v, nil
		}
		t, ok := mod.ThunkFor(imp.Key())
		if !ok {
			return 0, 0, fmt.Errorf("win16: 匯入 %s 沒有 thunk", imp.Key())
		}
		return t, ThunkSel, nil
	case ne.RelOSFixup:
		// WIN87EM 的浮點修補。沒有 FP 模擬器時填一個會炸的位址比填 0 好。
		return 0, ThunkSel, nil
	}
	return 0, 0, fmt.Errorf("win16: 未知的重定位種類 %d", r.Kind)
}

// applyReloc 走完一筆重定位的整條鏈。
//
// NE 的非加法式重定位是**鏈結串列**：被修補的那個 word 原本存的是下一處要
// 修補的位移，`0xFFFF` 結束。所以要先讀出鏈結、再覆寫，順序反了就會把鏈弄丟。
func (mod *Module) applyReloc(sel uint16, s ne.Segment, r ne.Reloc) error {
	off, seg, err := mod.relocValue(r)
	if err != nil {
		return err
	}

	if r.Additive {
		// 加法式：只有一處，把值加上去。
		mod.RelocsApplied++
		return mod.patch(sel, r.Offset, r.AddrType, off, seg, true)
	}

	mod.ChainsWalked++
	cur := r.Offset
	for n := 0; ; n++ {
		if n > 0x10000 {
			return fmt.Errorf("win16: 段 %d 的重定位鏈沒有終點（起點 0x%04X）", s.Index, r.Offset)
		}
		// 先讀下一個鏈結，再覆寫。
		next, err := mod.Mem.ReadU16(sel, cur)
		if err != nil {
			// 鏈結指到段外：NE 允許鏈結指向段尾之外表示結束，
			// 但更常見的是這個段沒配夠大。當成結束並記下來。
			break
		}
		if err := mod.patch(sel, cur, r.AddrType, off, seg, false); err != nil {
			return err
		}
		mod.RelocsApplied++
		if next == 0xFFFF {
			break
		}
		mod.ChainLinks++
		cur = next
	}
	return nil
}

func (mod *Module) patch(sel, at uint16, addrType uint8, off, seg uint16, additive bool) error {
	switch addrType {
	case ne.AddrLoByte:
		v := uint8(off)
		if additive {
			old, err := mod.Mem.ReadU8(sel, at)
			if err != nil {
				return err
			}
			v += old
		}
		return mod.Mem.WriteU8(sel, at, v)
	case ne.AddrOffset:
		v := off
		if additive {
			old, err := mod.Mem.ReadU16(sel, at)
			if err != nil {
				return err
			}
			v += old
		}
		return mod.Mem.WriteU16(sel, at, v)
	case ne.AddrSegment:
		return mod.Mem.WriteU16(sel, at, seg)
	case ne.AddrFarAddr:
		if err := mod.Mem.WriteU16(sel, at, off); err != nil {
			return err
		}
		return mod.Mem.WriteU16(sel, at+2, seg)
	}
	return fmt.Errorf("win16: 未知的重定位位址型別 %d", addrType)
}
