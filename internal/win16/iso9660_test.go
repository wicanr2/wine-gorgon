package win16

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// 造一份最小的 ISO9660：主卷冊描述表在第 16 格，根目錄在第 17 格，
// 裡面一個檔案在第 18 格。sector 指定實體版面（2048 或 2352）。
func makeISO(t *testing.T, sector, skip int, content []byte) string {
	t.Helper()
	const logical = 2048
	total := 18 + (len(content)+logical-1)/logical + 1
	img := make([]byte, total*sector)
	put := func(lba int, data []byte) {
		for i := 0; i < len(data); i++ {
			off := (lba+i/logical)*sector + skip + i%logical
			img[off] = data[i]
		}
	}
	pvd := make([]byte, logical)
	pvd[0] = 1
	copy(pvd[1:], "CD001")
	// 根目錄的目錄記錄在 +156。
	root := make([]byte, 34)
	root[0] = 34
	root[2], root[3], root[4], root[5] = 17, 0, 0, 0
	root[10], root[11] = byte(logical&0xFF), byte(logical>>8)
	root[25] = 2 // 目錄
	root[32] = 1
	copy(pvd[156:], root)
	put(16, pvd)

	// 根目錄內容：一筆 "HELLO.TXT;1"。
	name := "HELLO.TXT;1"
	rec := make([]byte, 33+len(name))
	rec[0] = byte(len(rec))
	rec[2] = 18 // LBA
	rec[10] = byte(len(content))
	rec[11] = byte(len(content) >> 8)
	rec[32] = byte(len(name))
	copy(rec[33:], name)
	dir := make([]byte, logical)
	copy(dir, rec)
	put(17, dir)

	body := make([]byte, logical*((len(content)+logical-1)/logical))
	copy(body, content)
	put(18, body)

	path := filepath.Join(t.TempDir(), "d.img")
	if err := os.WriteFile(path, img, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestISOBothLayouts(t *testing.T) {
	// 內容刻意跨過一個邏輯扇區：2352 版面下，直接算 offset 會讀到 ECC。
	content := make([]byte, 2048+16)
	for i := range content {
		content[i] = byte(i)
	}
	for _, l := range []struct {
		name   string
		sector int
		skip   int
	}{{"iso2048", 2048, 0}, {"bin2352", 2352, 16}} {
		t.Run(l.name, func(t *testing.T) {
			iso, err := OpenISO(makeISO(t, l.sector, l.skip, content))
			if err != nil {
				t.Fatal(err)
			}
			defer iso.Close()
			e, ok := iso.Lookup(`\HELLO.TXT`)
			if !ok {
				t.Fatal("找不到 HELLO.TXT")
			}
			if e.Size != len(content) {
				t.Fatalf("大小 %d，要 %d", e.Size, len(content))
			}
			f := &isoFile{iso: iso, base: int64(e.LBA) * isoLogicalSector, size: int64(e.Size)}
			got, err := io.ReadAll(f)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(content) {
				t.Fatalf("讀回來的內容不一樣（長度 %d）", len(got))
			}
		})
	}
}

func TestISOMountThroughFileSystem(t *testing.T) {
	iso, err := OpenISO(makeISO(t, 2352, 16, []byte("hi")))
	if err != nil {
		t.Fatal(err)
	}
	defer iso.Close()
	fs := NewFileSystem("", "")
	fs.ISOMounts = map[byte]*ISO{'D': iso}
	if !fs.Exists(`D:\HELLO.TXT`) {
		t.Fatal("D:\\HELLO.TXT 應該存在")
	}
	if fs.Exists(`D:\NOPE.TXT`) {
		t.Fatal("不存在的檔案不該說存在")
	}
	// 遊戲常用 OF_READWRITE 開唯讀來源；光碟要降級而不是失敗。
	h, err := fs.Open(`D:\HELLO.TXT`, 2)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := fs.File(h)
	b, _ := io.ReadAll(f)
	if string(b) != "hi" {
		t.Fatalf("內容是 %q", b)
	}
	if _, err := f.Write([]byte("x")); err == nil {
		t.Fatal("光碟應該不准寫")
	}
	fs.Close(h)
}
