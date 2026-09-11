package win16

import (
	"os"
	"path/filepath"
	"testing"
)

// 前綴只能整段剝。用 TrimPrefix 剝裸字串會把 `CIVDATA0.RSC` 變成
// `DATA0.RSC`，而症狀是遊戲說找不到自己的資料檔——看起來像遊戲的問題。
func TestPrefixStripsWholeComponentOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CIVDATA0.RSC"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileSystem(root, "CIV")
	for _, name := range []string{`CIVDATA0.RSC`, `C:\CIV\CIVDATA0.RSC`, `CivData0.rsc`} {
		h, err := fs.Open(name, 0)
		if err != nil {
			t.Errorf("開 %q 失敗：%v", name, err)
			continue
		}
		fs.Close(h)
	}
}

func TestOpenIsCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Sub", "Thing.Dat"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileSystem(root, "CIV")
	h, err := fs.Open(`C:\CIV\SUB\THING.DAT`, 0)
	if err != nil {
		t.Fatalf("大小寫不同就開不了：%v", err)
	}
	fs.Close(h)
}

// 沒有設定可寫目錄時一個 byte 都不准寫進原始資料。
func TestReadOnlyByDefault(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CIV.SAV"), []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileSystem(root, "CIV")
	if _, err := fs.Create(`C:\CIV\CIV.SAV`); err == nil {
		t.Fatal("沒有 WriteRoot 卻建得出檔")
	}
	b, _ := os.ReadFile(filepath.Join(root, "CIV.SAV"))
	if string(b) != "orig" {
		t.Fatalf("原始檔被動到了：%q", b)
	}
}

// 設了 WriteRoot 之後，寫入落在那裡，讀取先看那裡。
func TestWriteRootShadowsRoot(t *testing.T) {
	root, wr := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "A.DAT"), []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileSystem(root, "CIV")
	fs.WriteRoot = wr
	h, err := fs.Create(`A.DAT`)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := fs.File(h)
	_, _ = f.Write([]byte("new"))
	fs.Close(h)

	if b, _ := os.ReadFile(filepath.Join(root, "A.DAT")); string(b) != "orig" {
		t.Fatalf("原始檔被覆寫了：%q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(wr, "A.DAT")); string(b) != "new" {
		t.Fatalf("可寫目錄裡的內容是 %q", b)
	}
	h, err = fs.Open(`A.DAT`, 0)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	f, _ = fs.File(h)
	n, _ := f.Read(buf)
	if string(buf[:n]) != "new" {
		t.Errorf("讀到 %q，預期先看可寫目錄", buf[:n])
	}
	fs.Close(h)
}

// 有可寫目錄時，讀寫開一個只在原始目錄裡的檔：要複製到可寫目錄再開，
// 寫入落在副本，原始檔不動。PTO2 用 OF_READWRITE 開 RUMAP.TK2，沒有這一步
// 的症狀是「給了 -write 之後遊戲一開局就找不到 RUMAP.TK2」。
func TestReadWriteOpenCopiesOnWrite(t *testing.T) {
	root, wr := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "RUMAP.TK2"), []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileSystem(root, "PTO2")
	fs.WriteRoot = wr
	h, err := fs.Open(`RUMAP.TK2`, 2) // OF_READWRITE
	if err != nil {
		t.Fatalf("讀寫開檔失敗：%v", err)
	}
	f, _ := fs.File(h)
	buf := make([]byte, 4)
	if n, _ := f.Read(buf); string(buf[:n]) != "orig" {
		t.Fatalf("讀到 %q，預期原始內容", buf[:n])
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("NEW!")); err != nil {
		t.Fatal(err)
	}
	fs.Close(h)
	if b, _ := os.ReadFile(filepath.Join(root, "RUMAP.TK2")); string(b) != "orig" {
		t.Fatalf("原始檔被改了：%q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(wr, "RUMAP.TK2")); string(b) != "NEW!" {
		t.Fatalf("可寫目錄裡的副本是 %q", b)
	}
}

func TestMissingListsOnlyFailures(t *testing.T) {
	fs := NewFileSystem(t.TempDir(), "CIV")
	_, _ = fs.Open("NOPE.DAT", 0)
	_, _ = fs.Open("NOPE.DAT", 0)
	if got := fs.Missing(); len(got) != 1 || got[0] != "NOPE.DAT" {
		t.Errorf("找不到的清單是 %v", got)
	}
}
