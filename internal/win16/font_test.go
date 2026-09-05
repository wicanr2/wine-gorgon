package win16

import (
	"encoding/binary"
	"testing"
)

// buildFNT 造一份最小的 2.0 版 FNT：兩個字，寬度不同。
//
// 自己造而不是拿原版字型來測，是因為原版資料不進版控（AGENTS.md）。
// 這裡要驗的是**格式的讀法**，尤其是「字形是直行排列」這一點。
func buildFNT(t *testing.T) []byte {
	t.Helper()
	const height = 8
	b := make([]byte, 0x76)
	put16 := func(o int, v uint16) { binary.LittleEndian.PutUint16(b[o:], v) }
	put32 := func(o int, v uint32) { binary.LittleEndian.PutUint32(b[o:], v) }
	put16(0x00, 0x0200) // dfVersion
	put16(0x44, 8)      // dfPoints
	put16(0x4A, 6)      // dfAscent
	put16(0x4C, 1)      // dfInternalLeading
	put16(0x53, 400)    // dfWeight
	put16(0x58, height) // dfPixHeight
	put16(0x5B, 5)      // dfAvgWidth
	put16(0x5D, 9)      // dfMaxWidth
	b[0x5F] = 'A'       // dfFirstChar
	b[0x60] = 'B'       // dfLastChar
	b[0x61] = 0         // dfDefaultChar（相對 dfFirstChar）

	// 字表在 0x76：兩個字各 (寬 2 ＋ 位移 2)，再加一個哨兵。
	table := make([]byte, 3*4)
	glyphOff := 0x76 + len(table)

	// 'A'：寬 5，一個直條，第 0 列全亮、其餘只亮最左邊那一點。
	aBits := make([]byte, height)
	aBits[0] = 0xF8 // 11111000
	for y := 1; y < height; y++ {
		aBits[y] = 0x80
	}
	// 'B'：寬 9，跨兩個直條；第二條只有一個像素寬。
	bBits := make([]byte, 2*height)
	for y := 0; y < height; y++ {
		bBits[y] = 0x00        // 第一條全暗
		bBits[height+y] = 0x80 // 第二條的第 8 行亮著
	}

	binary.LittleEndian.PutUint16(table[0:], 5)
	binary.LittleEndian.PutUint16(table[2:], uint16(glyphOff))
	binary.LittleEndian.PutUint16(table[4:], 9)
	binary.LittleEndian.PutUint16(table[6:], uint16(glyphOff+len(aBits)))
	binary.LittleEndian.PutUint16(table[8:], 0)
	binary.LittleEndian.PutUint16(table[10:], 0)

	face := "TESTFONT\x00"
	b = append(b, table...)
	b = append(b, aBits...)
	b = append(b, bBits...)
	faceOff := len(b)
	b = append(b, face...)
	put32(0x69, uint32(faceOff)) // dfFace
	put32(0x02, uint32(len(b)))  // dfSize
	return b
}

func TestParseFNT(t *testing.T) {
	f, err := ParseFNT(buildFNT(t))
	if err != nil {
		t.Fatalf("解析失敗：%v", err)
	}
	if f.Face != "TESTFONT" {
		t.Errorf("字面名 %q", f.Face)
	}
	if f.Height != 8 || f.Ascent != 6 {
		t.Errorf("高 %d 上緣 %d，預期 8／6", f.Height, f.Ascent)
	}
	if f.CharWidth('A') != 5 || f.CharWidth('B') != 9 {
		t.Errorf("字寬 A=%d B=%d，預期 5／9", f.CharWidth('A'), f.CharWidth('B'))
	}
	if got := f.TextWidth("AB"); got != 14 {
		t.Errorf("\"AB\" 寬 %d，預期 14", got)
	}

	// 'A' 的第 0 列是最左邊五個像素。
	for x := 0; x < 5; x++ {
		if !f.Pixel('A', x, 0) {
			t.Errorf("A (%d,0) 應該亮", x)
		}
	}
	if f.Pixel('A', 1, 1) {
		t.Error("A (1,1) 應該暗")
	}
	if !f.Pixel('A', 0, 1) {
		t.Error("A (0,1) 應該亮")
	}

	// 'B' 只有第 8 行（第二個直條的最高位）是亮的。
	// 字形是**直行排列**：讀成一般的由左到右會在這裡整個錯開。
	if f.Pixel('B', 0, 0) {
		t.Error("B (0,0) 應該暗")
	}
	if !f.Pixel('B', 8, 0) {
		t.Error("B (8,0) 應該亮——直行排列讀錯的話這裡會是暗的")
	}
	if !f.Pixel('B', 8, 7) {
		t.Error("B (8,7) 應該亮")
	}

	// 範圍外的字退回預設字（這裡預設字就是 'A'）。
	if f.CharWidth('Z') != 5 {
		t.Errorf("超出範圍的字寬 %d，預期退回預設字的 5", f.CharWidth('Z'))
	}
}

func TestParseFNTRejectsGarbage(t *testing.T) {
	if _, err := ParseFNT(make([]byte, 16)); err == nil {
		t.Error("太短的資料應該被拒絕")
	}
	b := buildFNT(t)
	b[0], b[1] = 0x55, 0x55 // 亂版本
	if _, err := ParseFNT(b); err == nil {
		t.Error("不認得的版本應該被拒絕")
	}
}

// 字型選擇：名字優先，其次高度最接近。
func TestMatchFontPrefersFaceName(t *testing.T) {
	p := &Process{Fonts: []*BitmapFont{
		{Face: "CIVTIMES", Height: 12, Weight: 400},
		{Face: "CIVTIMES", Height: 18, Weight: 400},
		{Face: "System", Height: 18, Weight: 400},
	}}
	got := p.matchFont(&Font{FaceName: "civtimes", Height: 18})
	if got == nil || got.Face != "CIVTIMES" || got.Height != 18 {
		t.Errorf("挑到 %+v，預期 CIVTIMES/18", got)
	}
	got = p.matchFont(&Font{FaceName: "", Height: 12})
	if got == nil || got.Height != 12 {
		t.Errorf("沒指定字面名時挑到高 %v，預期 12", got)
	}
}
