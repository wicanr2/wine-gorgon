package ne

import (
	"encoding/binary"
	"testing"
)

// buildNE 組一個最小可解析的 NE：一個程式段、兩筆匯入重定位
// （一筆序號、一筆名稱），模組參考只有 GDI。
//
// 用合成映像而不是真的遊戲執行檔測試，是因為原版執行檔不能進版控。
// 對真檔的驗證用 `cmd/neinfo` 手動跑，數字記在 docs/spec/001。
func buildNE(t *testing.T) []byte {
	t.Helper()
	const (
		neOff      = 0x80
		segTable   = 0x40 // 相對 NE 檔頭
		modRefTab  = 0x50
		impNameTab = 0x60
		segData    = 0x200
		sectShift  = 4 // 1 sector = 16 bytes
	)
	buf := make([]byte, 0x400)
	copy(buf, "MZ")
	binary.LittleEndian.PutUint16(buf[0x3C:], neOff)
	copy(buf[neOff:], "NE")
	put := func(off int, v uint16) { binary.LittleEndian.PutUint16(buf[neOff+off:], v) }
	put(0x10, 0x0800) // heap
	put(0x12, 0x1000) // stack
	put(0x14, 0x0123) // IP
	put(0x16, 1)      // CS ＝ 段 1
	put(0x1C, 1)      // 段數
	put(0x1E, 1)      // 模組參考數
	put(0x22, segTable)
	put(0x28, modRefTab)
	put(0x2A, impNameTab)
	put(0x32, sectShift)

	// 段表：sector、長度、旗標（bit8 ＝ 有重定位）、minalloc
	st := neOff + segTable
	binary.LittleEndian.PutUint16(buf[st:], segData>>sectShift)
	binary.LittleEndian.PutUint16(buf[st+2:], 0x10)
	binary.LittleEndian.PutUint16(buf[st+4:], 0x0100)
	binary.LittleEndian.PutUint16(buf[st+6:], 0x10)

	// 模組參考表：一筆，指向 imported-name table 裡的 "GDI"
	binary.LittleEndian.PutUint16(buf[neOff+modRefTab:], 0x01)

	// imported-name table：位移 0 留白（規格要求），0x01 起是長度前綴字串
	nt := neOff + impNameTab
	buf[nt] = 0
	buf[nt+1] = 3
	copy(buf[nt+2:], "GDI")
	buf[nt+5] = 6
	copy(buf[nt+6:], "BITBLT")

	// 重定位表接在段內容之後
	rel := segData + 0x10
	binary.LittleEndian.PutUint16(buf[rel:], 2)
	// 第一筆：IMPORTORDINAL，GDI #45
	r := rel + 2
	buf[r] = AddrFarAddr
	buf[r+1] = RelImportOrd
	binary.LittleEndian.PutUint16(buf[r+2:], 0x0004)
	binary.LittleEndian.PutUint16(buf[r+4:], 1)  // 模組 1
	binary.LittleEndian.PutUint16(buf[r+6:], 45) // 序號
	// 第二筆：IMPORTNAME，GDI.BITBLT
	r += 8
	buf[r] = AddrFarAddr
	buf[r+1] = RelImportName
	binary.LittleEndian.PutUint16(buf[r+2:], 0x0008)
	binary.LittleEndian.PutUint16(buf[r+4:], 1)
	binary.LittleEndian.PutUint16(buf[r+6:], 5) // "BITBLT" 在名稱表的位移
	return buf
}

func TestParseMinimal(t *testing.T) {
	img, err := Parse(buildNE(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := len(img.Segments); got != 1 {
		t.Fatalf("段數 %d，want 1", got)
	}
	if got := img.ModuleNames; len(got) != 1 || got[0] != "GDI" {
		t.Fatalf("模組參考 %v，want [GDI]", got)
	}
	seg, off, err := img.Entry()
	if err != nil || seg != 1 || off != 0x0123 {
		t.Fatalf("進入點 %d:%04X err=%v，want 1:0123", seg, off, err)
	}
	if got := len(img.Segments[0].Relocs); got != 2 {
		t.Fatalf("重定位 %d 筆，want 2", got)
	}
	if got := len(img.Imports); got != 2 {
		t.Fatalf("相異匯入 %d 項，want 2", got)
	}
	keys := map[string]bool{}
	for _, imp := range img.Imports {
		keys[imp.Key()] = true
		if imp.Refs != 1 {
			t.Errorf("%s 引用數 %d，want 1", imp.Key(), imp.Refs)
		}
	}
	for _, want := range []string{"GDI.#45", "GDI.BITBLT"} {
		if !keys[want] {
			t.Errorf("缺匯入 %s（實際 %v）", want, keys)
		}
	}
}

// TestParseRejectsGarbage 是負對照：解析器要真的在檢查，不是照單全收。
func TestParseRejectsGarbage(t *testing.T) {
	for name, in := range map[string][]byte{
		"太短":    []byte("MZ"),
		"不是 MZ": make([]byte, 0x200),
		"NE 位移壞": func() []byte {
			b := make([]byte, 0x200)
			copy(b, "MZ")
			binary.LittleEndian.PutUint16(b[0x3C:], 0xFFF0)
			return b
		}(),
	} {
		if _, err := Parse(in); err == nil {
			t.Errorf("%s：預期報錯，卻成功了", name)
		}
	}
}

// TestImportKey 鎖住兩種匯入的顯示形狀——序號式與名稱式在報告裡要分得出來。
func TestImportKey(t *testing.T) {
	if got := (Import{Module: "GDI", Ordinal: 45}).Key(); got != "GDI.#45" {
		t.Errorf("序號式 Key ＝ %q", got)
	}
	if got := (Import{Module: "USER", Name: "TEXTOUT"}).Key(); got != "USER.TEXTOUT" {
		t.Errorf("名稱式 Key ＝ %q", got)
	}
}
