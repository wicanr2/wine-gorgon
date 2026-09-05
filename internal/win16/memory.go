// Package win16 把一個 NE 映像鋪成可執行的位址空間：每個段一個 selector、
// 走完重定位鏈、把匯入目標換成可攔截的 thunk。
package win16

import "fmt"

// Memory 是 selector 定址的記憶體。
//
// **不做 LDT**（`docs/spec/001` §3）：Win16 應用程式看不到 descriptor 的內容，
// 所以 selector 在這裡只是「一塊記憶體的 handle」，查表就夠。這個決定讓整層
// 落在可完成的篇幅內，代價是程式若自己造 descriptor（`AllocSelector` 那一族）
// 就得另外處理——CIV.EXE 的匯入表裡沒有，所以先不做。
type Memory struct {
	blocks map[uint16]*Block
	next   uint16 // 下一個要發的動態 selector
}

// Block 是一塊有 selector 的記憶體。
type Block struct {
	Sel   uint16
	Data  []byte
	Name  string // 給錯誤訊息用：`seg 12`、`thunk`、`GlobalAlloc#3`
	Fixed bool   // 段是不是不可移動（目前只影響顯示）
}

// SelInvalidError 是碰到沒配過的 selector。
type SelInvalidError struct {
	Sel uint16
	Op  string
}

func (e *SelInvalidError) Error() string {
	return fmt.Sprintf("win16: %s 用了未配置的 selector %04X", e.Op, e.Sel)
}

// NewMemory 建一份空的位址空間。動態 selector 從 0x8007 起發
// （低三位是 LDT 的 TI＋RPL，純粹為了讓數字看起來像真的 selector）。
func NewMemory() *Memory {
	return &Memory{blocks: map[uint16]*Block{}, next: 0x8007}
}

// SegSelector 是段號（1-based）對應的 selector。
//
// 刻意讓 `sel>>3` 就是段號：除錯時看到 `0x0217` 立刻知道是段 66，
// 不必回頭查表。
func SegSelector(index int) uint16 { return uint16(index)<<3 | 7 }

// SelSegment 是 SegSelector 的反函式；回 0 表示不是段 selector。
func SelSegment(sel uint16) int {
	if sel&7 != 7 || sel >= 0x8000 {
		return 0
	}
	return int(sel >> 3)
}

// Put 把一塊記憶體掛到指定 selector 上。
func (m *Memory) Put(sel uint16, name string, data []byte) *Block {
	b := &Block{Sel: sel, Data: data, Name: name}
	m.blocks[sel] = b
	return b
}

// Alloc 配一塊新的記憶體並發一個 selector。
func (m *Memory) Alloc(name string, size int) *Block {
	sel := m.next
	m.next += 8
	return m.Put(sel, name, make([]byte, size))
}

// Block 取回 selector 對應的區塊。
func (m *Memory) Block(sel uint16) (*Block, bool) {
	b, ok := m.blocks[sel]
	return b, ok
}

// Blocks 回傳目前所有區塊（順序不保證）。
func (m *Memory) Blocks() map[uint16]*Block { return m.blocks }

// bounds 檢查 selector 與段內位移。
//
// 位移**不環繞**：真的 8086 在段內是 mod 0x10000，但那個環繞在保護模式下
// 是 GP fault，而且在這裡幾乎一定是 bug 而不是刻意。回錯比默默讀到別的地方好。
func (m *Memory) bounds(sel uint16, off int, n int, op string) (*Block, error) {
	b, ok := m.blocks[sel]
	if !ok {
		return nil, &SelInvalidError{Sel: sel, Op: op}
	}
	if off < 0 || off+n > len(b.Data) {
		return nil, fmt.Errorf("win16: %s 越界：%s(%04X) 長度 0x%X，要 [0x%X, 0x%X)",
			op, b.Name, sel, len(b.Data), off, off+n)
	}
	return b, nil
}

// ReadU8／ReadU16／WriteU8／WriteU16 是所有記憶體存取的唯一入口。
func (m *Memory) ReadU8(sel uint16, off uint16) (uint8, error) {
	b, err := m.bounds(sel, int(off), 1, "讀 byte")
	if err != nil {
		return 0, err
	}
	return b.Data[off], nil
}

func (m *Memory) ReadU16(sel uint16, off uint16) (uint16, error) {
	b, err := m.bounds(sel, int(off), 2, "讀 word")
	if err != nil {
		return 0, err
	}
	return uint16(b.Data[off]) | uint16(b.Data[off+1])<<8, nil
}

func (m *Memory) WriteU8(sel uint16, off uint16, v uint8) error {
	b, err := m.bounds(sel, int(off), 1, "寫 byte")
	if err != nil {
		return err
	}
	b.Data[off] = v
	return nil
}

func (m *Memory) WriteU16(sel uint16, off uint16, v uint16) error {
	b, err := m.bounds(sel, int(off), 2, "寫 word")
	if err != nil {
		return err
	}
	b.Data[off] = uint8(v)
	b.Data[off+1] = uint8(v >> 8)
	return nil
}
