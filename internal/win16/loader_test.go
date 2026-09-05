package win16

import (
	"encoding/binary"
	"testing"

	"github.com/wicanr2/wine-gorgon/internal/ne"
)

// buildChained 組一個**有多節重定位鏈**的 NE。
//
// 這個合成映像存在的理由很具體：CIV.EXE 的 10,429 條非加法式重定位**全部**
// 是單節（Borland TLINK 一筆只寫一處，鏈結欄一律 0xFFFF），所以走鏈那段
// 程式碼在真檔上一次都跑不到。只靠真檔驗證會讓那段永遠沒有覆蓋。
func buildChained(t *testing.T) []byte {
	t.Helper()
	const (
		neOff      = 0x80
		segTable   = 0x40
		modRefTab  = 0x50
		impNameTab = 0x60
		segData    = 0x200
		segLen     = 0x20
		sectShift  = 4
	)
	buf := make([]byte, 0x400)
	copy(buf, "MZ")
	binary.LittleEndian.PutUint16(buf[0x3C:], neOff)
	copy(buf[neOff:], "NE")
	put := func(off int, v uint16) { binary.LittleEndian.PutUint16(buf[neOff+off:], v) }
	put(0x14, 0x0000) // IP
	put(0x16, 1)      // CS = 段 1
	put(0x1C, 1)      // 段數
	put(0x1E, 1)      // 模組參考
	put(0x22, segTable)
	put(0x28, modRefTab)
	put(0x2A, impNameTab)
	put(0x32, sectShift)

	st := neOff + segTable
	binary.LittleEndian.PutUint16(buf[st:], segData>>sectShift)
	binary.LittleEndian.PutUint16(buf[st+2:], segLen)
	binary.LittleEndian.PutUint16(buf[st+4:], 0x0100) // 有重定位
	binary.LittleEndian.PutUint16(buf[st+6:], segLen)

	binary.LittleEndian.PutUint16(buf[neOff+modRefTab:], 0x01)
	nt := neOff + impNameTab
	buf[nt] = 0
	buf[nt+1] = 3
	copy(buf[nt+2:], "GDI")

	// 段內容：0x00 起是一條鏈 0x00 → 0x08 → 0x10 → 0xFFFF（結束）。
	// 每一格是 far pointer 的低 word，也就是鏈結欄。
	binary.LittleEndian.PutUint16(buf[segData+0x00:], 0x0008)
	binary.LittleEndian.PutUint16(buf[segData+0x08:], 0x0010)
	binary.LittleEndian.PutUint16(buf[segData+0x10:], 0xFFFF)

	rel := segData + segLen
	binary.LittleEndian.PutUint16(buf[rel:], 1)
	r := rel + 2
	buf[r] = ne.AddrFarAddr
	buf[r+1] = ne.RelImportOrd
	binary.LittleEndian.PutUint16(buf[r+2:], 0x0000) // 鏈的起點
	binary.LittleEndian.PutUint16(buf[r+4:], 1)      // 模組 1 ＝ GDI
	binary.LittleEndian.PutUint16(buf[r+6:], 45)     // 序號
	return buf
}

func TestLoadWalksRelocationChain(t *testing.T) {
	img, err := ne.Parse(buildChained(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	mod, err := Load(img)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if mod.ChainsWalked != 1 {
		t.Fatalf("走了 %d 條鏈，want 1", mod.ChainsWalked)
	}
	if mod.RelocsApplied != 3 {
		t.Fatalf("修補 %d 處，want 3（鏈有三節）", mod.RelocsApplied)
	}
	if mod.ChainLinks != 2 {
		t.Fatalf("鏈上第二處以後 %d，want 2", mod.ChainLinks)
	}

	// 三處都要被填成同一個 thunk far pointer。
	want, ok := mod.ThunkFor("GDI.#45")
	if !ok {
		t.Fatal("GDI.#45 沒有 thunk")
	}
	sel := SegSelector(1)
	for _, at := range []uint16{0x00, 0x08, 0x10} {
		off, err := mod.Mem.ReadU16(sel, at)
		if err != nil {
			t.Fatalf("讀 %04X: %v", at, err)
		}
		seg, err := mod.Mem.ReadU16(sel, at+2)
		if err != nil {
			t.Fatalf("讀 %04X+2: %v", at, err)
		}
		if off != want || seg != ThunkSel {
			t.Errorf("位置 %04X 填成 %04X:%04X，want %04X:%04X", at, seg, off, ThunkSel, want)
		}
	}
}

// TestImportAt 是攔截點的反查：CPU 只知道自己跳到 ThunkSel:off，
// 要能問回「這是哪一個 API」。
func TestImportAt(t *testing.T) {
	img, err := ne.Parse(buildChained(t))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := Load(img)
	if err != nil {
		t.Fatal(err)
	}
	off, _ := mod.ThunkFor("GDI.#45")
	imp, ok := mod.ImportAt(off)
	if !ok || imp.Key() != "GDI.#45" {
		t.Fatalf("ImportAt(%04X) ＝ %q, %v", off, imp.Key(), ok)
	}
	// 沒對齊 ThunkStride 的位移要拒絕，而不是回一個看起來合理的鄰居。
	if _, ok := mod.ImportAt(off + 1); ok {
		t.Error("沒對齊的位移應該回 false")
	}
}

// TestSelectorRoundTrip 鎖住「selector >> 3 就是段號」這個除錯用的約定。
func TestSelectorRoundTrip(t *testing.T) {
	for _, i := range []int{1, 2, 66, 133} {
		if got := SelSegment(SegSelector(i)); got != i {
			t.Errorf("段 %d 的 selector 反查成 %d", i, got)
		}
	}
	if SelSegment(ThunkSel) != 0 {
		t.Error("thunk selector 不該被當成段")
	}
}
