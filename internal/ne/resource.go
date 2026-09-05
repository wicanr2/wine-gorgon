package ne

import "fmt"

// Resource 是資源表裡的一項。
//
// NE 的資源表把長度與位移都存成「對齊單位」的個數，單位大小由表頭的
// `rscAlignShift` 決定。這一層在解析時就換算成位元組，外面不必再乘。
type Resource struct {
	TypeID   uint16 // 高位元被清掉之後的數字型別；TypeName 非空時無意義
	TypeName string
	ID       uint16 // 數字 ID；Name 非空時無意義
	Name     string
	Offset   uint32 // 檔案位移（位元組）
	Length   uint32 // 位元組
	Flags    uint16
}

// 常見的資源型別編號。
const (
	RTCursor      = 1
	RTBitmap      = 2
	RTIcon        = 3
	RTMenu        = 4
	RTDialog      = 5
	RTString      = 6
	RTFontDir     = 7
	RTFont        = 8
	RTAccelerator = 9
	RTRCData      = 10
	RTGroupCursor = 12
	RTGroupIcon   = 14
)

// parseResources 讀資源表。表空著（`rsrcTab == restab`）是合法的。
func (img *Image) parseResources(rsrcOff, resNameOff uint32) error {
	if rsrcOff >= resNameOff {
		return nil // 沒有資源表
	}
	shift, err := u16(img.raw, rsrcOff)
	if err != nil {
		return err
	}
	pos := rsrcOff + 2
	for {
		typeID, err := u16(img.raw, pos)
		if err != nil {
			return err
		}
		if typeID == 0 {
			return nil
		}
		count, err := u16(img.raw, pos+2)
		if err != nil {
			return err
		}
		pos += 8 // rtTypeID(2) + rtResourceCount(2) + rtReserved(4)

		typeName := ""
		if typeID&0x8000 == 0 {
			typeName, err = pascalString(img.raw, rsrcOff+uint32(typeID))
			if err != nil {
				return err
			}
		}

		for i := uint16(0); i < count; i++ {
			off, err := u16(img.raw, pos)
			if err != nil {
				return err
			}
			length, err := u16(img.raw, pos+2)
			if err != nil {
				return err
			}
			flags, err := u16(img.raw, pos+4)
			if err != nil {
				return err
			}
			id, err := u16(img.raw, pos+6)
			if err != nil {
				return err
			}
			r := Resource{
				TypeID: typeID &^ 0x8000, TypeName: typeName,
				ID:     id &^ 0x8000,
				Offset: uint32(off) << shift,
				Length: uint32(length) << shift,
				Flags:  flags,
			}
			if id&0x8000 == 0 {
				r.Name, err = pascalString(img.raw, rsrcOff+uint32(id))
				if err != nil {
					return err
				}
			}
			img.Resources = append(img.Resources, r)
			pos += 12
		}
	}
}

// ResourceData 回傳一項資源的位元組。
func (img *Image) ResourceData(r Resource) ([]byte, error) {
	end := r.Offset + r.Length
	if int(end) > len(img.raw) || end < r.Offset {
		return nil, errf("資源 %s 超出檔尾（%d..%d，檔長 %d）", r, r.Offset, end, len(img.raw))
	}
	return img.raw[r.Offset:end], nil
}

// FindResource 依型別與名稱／編號找資源。name 以 `#` 開頭表示數字。
func (img *Image) FindResource(typeID uint16, typeName, name string, id uint16) (Resource, bool) {
	for _, r := range img.Resources {
		if typeName != "" {
			if !equalFold(r.TypeName, typeName) {
				continue
			}
		} else if r.TypeName != "" || r.TypeID != typeID {
			continue
		}
		if name != "" {
			if equalFold(r.Name, name) {
				return r, true
			}
			continue
		}
		if r.Name == "" && r.ID == id {
			return r, true
		}
	}
	return Resource{}, false
}

func (r Resource) String() string {
	t := fmt.Sprintf("#%d", r.TypeID)
	if r.TypeName != "" {
		t = r.TypeName
	}
	n := fmt.Sprintf("#%d", r.ID)
	if r.Name != "" {
		n = r.Name
	}
	return t + "/" + n
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'a' && x <= 'z' {
			x -= 32
		}
		if y >= 'a' && y <= 'z' {
			y -= 32
		}
		if x != y {
			return false
		}
	}
	return true
}
