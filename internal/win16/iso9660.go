package win16

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ISO 是一張光碟資料軌的唯讀檔案系統。
//
// 為什麼要直接讀映像而不先解到目錄裡：CD 遊戲的資料軌動輒一兩百 MB，
// 解一份出來就多一份會走味的副本——原版素材只能有一個真相，而那個
// 真相是使用者自己那張 CD 的映像。
//
// 兩種扇區版面都吃：純 `.iso`（2048 一格）與 `MODE1/2352` 的 `.bin`
// （2352 一格，前 16 byte 是同步與標頭、後 288 byte 是 ECC）。版面是
// **量出來的**——分別假設一種去讀第 16 格的主卷冊描述表，看哪一種的
// 前五個 byte 是 `CD001`。
type ISO struct {
	f         *os.File
	sector    int // 實體扇區大小
	skip      int // 每格前面要跳過幾個 byte
	rootLBA   int
	rootBytes int
}

// isoLogicalSector 是 ISO9660 的邏輯扇區大小，固定 2048。
const isoLogicalSector = 2048

// OpenISO 打開一份光碟映像。
func OpenISO(path string) (*ISO, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	iso := &ISO{f: f}
	for _, layout := range [][2]int{{isoLogicalSector, 0}, {2352, 16}} {
		iso.sector, iso.skip = layout[0], layout[1]
		pvd := make([]byte, isoLogicalSector)
		if _, err := iso.ReadAt(pvd, int64(16*isoLogicalSector)); err != nil {
			continue
		}
		if string(pvd[1:6]) != "CD001" || pvd[0] != 1 {
			continue
		}
		// 主卷冊描述表 +156 是根目錄的目錄記錄。
		root := pvd[156 : 156+34]
		iso.rootLBA = int(le32(root[2:]))
		iso.rootBytes = int(le32(root[10:]))
		return iso, nil
	}
	f.Close()
	return nil, fmt.Errorf("%s：不是 ISO9660（2048 與 2352 兩種版面都沒讀到 CD001）", path)
}

// Close 關掉映像。
func (iso *ISO) Close() error { return iso.f.Close() }

// ReadAt 讀邏輯位址。跨扇區時自動接起來——2352 版面的每一格中間有
// 16＋288 個 byte 不屬於資料，直接算 offset 會讀到 ECC。
func (iso *ISO) ReadAt(p []byte, off int64) (int, error) {
	done := 0
	for done < len(p) {
		lba := (off + int64(done)) / isoLogicalSector
		within := (off + int64(done)) % isoLogicalSector
		n := isoLogicalSector - int(within)
		if n > len(p)-done {
			n = len(p) - done
		}
		phys := lba*int64(iso.sector) + int64(iso.skip) + within
		got, err := iso.f.ReadAt(p[done:done+n], phys)
		done += got
		if err != nil {
			return done, err
		}
	}
	return done, nil
}

// isoEntry 是一筆目錄記錄。
type isoEntry struct {
	Name  string
	LBA   int
	Size  int
	IsDir bool
}

// readDir 讀一個目錄的全部記錄。
func (iso *ISO) readDir(lba, size int) ([]isoEntry, error) {
	buf := make([]byte, size)
	if _, err := iso.ReadAt(buf, int64(lba)*isoLogicalSector); err != nil {
		return nil, err
	}
	var out []isoEntry
	for i := 0; i < len(buf); {
		ln := int(buf[i])
		if ln == 0 {
			// 目錄記錄不跨扇區；剩下的是填充，跳到下一格。
			i = (i/isoLogicalSector + 1) * isoLogicalSector
			continue
		}
		if i+ln > len(buf) {
			break
		}
		rec := buf[i : i+ln]
		nameLen := int(rec[32])
		if 33+nameLen > len(rec) {
			break
		}
		name := string(rec[33 : 33+nameLen])
		// `.` 與 `..` 的名字是單一 byte 0x00／0x01。
		if nameLen == 1 && (name[0] == 0 || name[0] == 1) {
			i += ln
			continue
		}
		out = append(out, isoEntry{
			Name:  strings.TrimSuffix(name, ";1"),
			LBA:   int(le32(rec[2:])),
			Size:  int(le32(rec[10:])),
			IsDir: rec[25]&2 != 0,
		})
		i += ln
	}
	return out, nil
}

// Lookup 解析一條 `\` 或 `/` 分隔的路徑。不分大小寫。
func (iso *ISO) Lookup(path string) (isoEntry, bool) {
	cur := isoEntry{LBA: iso.rootLBA, Size: iso.rootBytes, IsDir: true}
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	for _, part := range parts {
		if !cur.IsDir {
			return isoEntry{}, false
		}
		entries, err := iso.readDir(cur.LBA, cur.Size)
		if err != nil {
			return isoEntry{}, false
		}
		found := false
		for _, e := range entries {
			if strings.EqualFold(e.Name, part) {
				cur, found = e, true
				break
			}
		}
		if !found {
			return isoEntry{}, false
		}
	}
	return cur, len(parts) > 0
}

// isoFile 是 ISO 上的一個開著的檔案。它只讀。
type isoFile struct {
	iso  *ISO
	base int64
	size int64
	pos  int64
}

func (f *isoFile) Read(p []byte) (int, error) {
	if f.pos >= f.size {
		return 0, io.EOF
	}
	if int64(len(p)) > f.size-f.pos {
		p = p[:f.size-f.pos]
	}
	n, err := f.iso.ReadAt(p, f.base+f.pos)
	f.pos += int64(n)
	if err == io.EOF && n > 0 {
		err = nil
	}
	return n, err
}

func (f *isoFile) Write([]byte) (int, error) {
	return 0, errors.New("光碟是唯讀的")
}

func (f *isoFile) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.pos = off
	case io.SeekCurrent:
		f.pos += off
	case io.SeekEnd:
		f.pos = f.size + off
	default:
		return 0, fmt.Errorf("seek whence %d 無效", whence)
	}
	if f.pos < 0 {
		f.pos = 0
	}
	return f.pos, nil
}

func (f *isoFile) Close() error { return nil }

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
