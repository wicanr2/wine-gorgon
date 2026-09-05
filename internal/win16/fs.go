package win16

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileSystem 把 DOS 路徑對到主機目錄。
//
// 兩個刻意的限制：
//
//   - **預設唯讀。** 原版資料不能被工具改到（`AGENTS.md`）。要寫檔就給
//     一個獨立的 WriteRoot，寫入永遠落在那裡，讀取則是「先看 WriteRoot
//     再看 Root」——存檔覆寫原始檔的情形不會發生。
//   - **檔名比對不分大小寫。** DOS 沒有大小寫，Linux 有；不做這一步的話
//     `CIV.PIC` 在 `civ.pic` 旁邊就找不到，而且症狀是「遊戲說找不到檔案」，
//     看起來像遊戲的問題。
type FileSystem struct {
	Root      string // 唯讀的原始資料目錄
	WriteRoot string // 可寫目錄；空字串表示不准寫
	Drive     byte   // 這個根目錄掛在哪個磁碟機，預設 'C'
	Prefix    string // 根目錄對應的 DOS 目錄，例如 `CIV`

	files map[uint16]*openFile
	next  uint16

	// Opened 記下每一次開檔（成功與失敗都記）。找不到檔案是這一層最
	// 常見的失敗，而且遊戲通常只會回一句沒頭沒尾的錯誤訊息。
	Opened []OpenRecord
}

// OpenRecord 是一次開檔的紀錄。
type OpenRecord struct {
	DOSPath  string
	HostPath string
	Mode     int
	OK       bool
}

type openFile struct {
	f    *os.File
	name string
}

// NewFileSystem 造一個掛在 `C:\<prefix>` 的唯讀檔案系統。
func NewFileSystem(root, prefix string) *FileSystem {
	return &FileSystem{
		Root: root, Drive: 'C', Prefix: strings.ToUpper(prefix),
		files: map[uint16]*openFile{}, next: 5, // 0..4 留給標準串流
	}
}

// hostPath 把 DOS 路徑換成主機路徑。找不到就回空字串。
func (fs *FileSystem) hostPath(dos string, forWrite bool) string {
	rel := strings.TrimPrefix(strings.ToUpper(dos), fmt.Sprintf("%c:", fs.Drive))
	rel = strings.ReplaceAll(rel, "\\", "/")
	rel = strings.TrimPrefix(rel, "/")
	// 只在「整個路徑元件」相符時才剝掉前綴。用 TrimPrefix 剝裸字串會把
	// `CIVDATA0.RSC` 剝成 `DATA0.RSC`，而症狀是「遊戲找不到自己的資料檔」，
	// 看起來像遊戲的問題。
	if fs.Prefix != "" {
		if rel == fs.Prefix {
			rel = ""
		} else if strings.HasPrefix(rel, fs.Prefix+"/") {
			rel = rel[len(fs.Prefix)+1:]
		}
	}
	if rel == "" {
		return ""
	}
	if forWrite {
		if fs.WriteRoot == "" {
			return ""
		}
		return filepath.Join(fs.WriteRoot, rel)
	}
	if fs.WriteRoot != "" {
		if p := resolveCase(fs.WriteRoot, rel); p != "" {
			return p
		}
	}
	return resolveCase(fs.Root, rel)
}

// resolveCase 一層一層地做不分大小寫的比對。
func resolveCase(root, rel string) string {
	cur := root
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." {
			continue
		}
		direct := filepath.Join(cur, part)
		if _, err := os.Stat(direct); err == nil {
			cur = direct
			continue
		}
		entries, err := os.ReadDir(cur)
		if err != nil {
			return ""
		}
		found := ""
		for _, e := range entries {
			if strings.EqualFold(e.Name(), part) {
				found = filepath.Join(cur, e.Name())
				break
			}
		}
		if found == "" {
			return ""
		}
		cur = found
	}
	return cur
}

// Exists 回答一個 DOS 路徑存不存在（不開檔）。
func (fs *FileSystem) Exists(dos string) bool {
	if p := fs.hostPath(dos, false); p != "" {
		_, err := os.Stat(p)
		return err == nil
	}
	return false
}

// Open 開一個檔；mode 是 OpenFile 的 OF_* 低兩位（0 讀、1 寫、2 讀寫）。
func (fs *FileSystem) Open(dos string, mode int) (uint16, error) {
	write := mode&3 != 0
	host := fs.hostPath(dos, write)
	rec := OpenRecord{DOSPath: dos, HostPath: host, Mode: mode}
	if host == "" {
		fs.Opened = append(fs.Opened, rec)
		return 0, fmt.Errorf("找不到 %s", dos)
	}
	flag := os.O_RDONLY
	if mode&3 == 1 {
		flag = os.O_WRONLY
	} else if mode&3 == 2 {
		flag = os.O_RDWR
	}
	f, err := os.OpenFile(host, flag, 0o644)
	if err != nil {
		fs.Opened = append(fs.Opened, rec)
		return 0, err
	}
	rec.OK = true
	fs.Opened = append(fs.Opened, rec)
	h := fs.next
	fs.next++
	fs.files[h] = &openFile{f: f, name: host}
	return h, nil
}

// Create 建一個新檔（只能落在 WriteRoot）。
func (fs *FileSystem) Create(dos string) (uint16, error) {
	host := fs.hostPath(dos, true)
	if host == "" {
		return 0, fmt.Errorf("不准寫入 %s（沒有設定可寫目錄）", dos)
	}
	if err := os.MkdirAll(filepath.Dir(host), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(host)
	if err != nil {
		return 0, err
	}
	h := fs.next
	fs.next++
	fs.files[h] = &openFile{f: f, name: host}
	fs.Opened = append(fs.Opened, OpenRecord{DOSPath: dos, HostPath: host, Mode: 1, OK: true})
	return h, nil
}

// File 取一個開著的檔。
func (fs *FileSystem) File(h uint16) (*os.File, bool) {
	of, ok := fs.files[h]
	if !ok {
		return nil, false
	}
	return of.f, true
}

// Close 關掉一個檔。
func (fs *FileSystem) Close(h uint16) bool {
	of, ok := fs.files[h]
	if !ok {
		return false
	}
	_ = of.f.Close()
	delete(fs.files, h)
	return true
}

// CloseAll 收尾用。
func (fs *FileSystem) CloseAll() {
	for h := range fs.files {
		fs.Close(h)
	}
}

// Missing 回報找不到的檔案清單（去重、排序）。
func (fs *FileSystem) Missing() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range fs.Opened {
		if r.OK || seen[r.DOSPath] {
			continue
		}
		seen[r.DOSPath] = true
		out = append(out, r.DOSPath)
	}
	sort.Strings(out)
	return out
}
