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

	// HugeNext 是同一塊 huge 配置的下一個 selector；0 表示這是最後一段。
	// 超過 64 KiB 的配置在 Win16 是「連號 selector，每個對到 64 KiB」，
	// 程式用 `__AHSHIFT` 自己算要跳到哪一號。
	HugeNext uint16
	// HugeFirst 是這塊所屬 huge 配置的第一個 selector；0 表示不是 huge。
	HugeFirst uint16
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
	return &Memory{blocks: map[uint16]*Block{}, next: dynSelFirst}
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

// Alloc 配一塊新的記憶體並發一個 selector；發不出來回 nil。
func (m *Memory) Alloc(name string, size int) *Block {
	sel, ok := m.nextSel()
	if !ok {
		return nil
	}
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

// AllocHuge 配置超過 64 KiB 的一塊，回傳第一個 selector。
//
// 每 64 KiB 一個 selector、號碼連續（每次 +8，也就是 `__AHSHIFT` ＝ 3）。
// 所有段共用同一個底層陣列，所以跨段寫入天然是連續的——這是 Go 的
// slice 剛好對得上 Win16 huge 模型的地方。
func (m *Memory) AllocHuge(name string, size int) *Block {
	if size <= 0 {
		size = 1
	}
	backing := make([]byte, size)
	const chunk = 0x10000
	n := (size + chunk - 1) / chunk

	var first *Block
	var prev *Block
	for i := 0; i < n; i++ {
		lo := i * chunk
		hi := lo + chunk
		if hi > size {
			hi = size
		}
		sel, ok := m.nextSel()
		if !ok {
			return nil
		}
		b := m.Put(sel, fmt.Sprintf("%s#%d", name, i), backing[lo:hi])
		if first == nil {
			first = b
		}
		b.HugeFirst = first.Sel
		if prev != nil {
			prev.HugeNext = b.Sel
		}
		prev = b
	}
	return first
}

// 動態 selector 的範圍。低位三個 bit 固定是 7（LDT ＋ RPL），所以每一格
// 相差 8——這也正是 `__AHSHIFT` ＝ 3 的意思。
const (
	dynSelFirst = 0x8007
	dynSelLast  = 0xFFF7
)

// nextSel 發一個沒被用掉的動態 selector。
//
// **一定要跳過已經在用的，而且不能讓 uint16 自己繞回去。** 早期版本是
// `m.next += 8`，配到第 8,192 個時 uint16 溢位變成 `0x0007`、`0x000F`……
// 也就是段 0、段 1 的 selector，於是一塊 GlobalAlloc 把程式碼段整個蓋掉。
// 症狀出現在幾千萬條指令之後，而且是「取指失敗」——離肇因非常遠。
func (m *Memory) nextSel() (uint16, bool) {
	start := m.next
	for {
		sel := m.next
		if m.next >= dynSelLast {
			m.next = dynSelFirst
		} else {
			m.next += 8
		}
		if _, used := m.blocks[sel]; !used {
			return sel, true
		}
		if m.next == start {
			return 0, false // 繞了一圈都沒有空位
		}
	}
}

// FreeHuge 釋放一整塊 huge 配置。
func (m *Memory) FreeHuge(sel uint16) bool {
	b, ok := m.blocks[sel]
	if !ok {
		return false
	}
	if b.HugeFirst == 0 {
		return m.Free(sel)
	}
	cur := b.HugeFirst
	for cur != 0 {
		nxt := uint16(0)
		if blk, ok := m.blocks[cur]; ok {
			nxt = blk.HugeNext
		}
		delete(m.blocks, cur)
		cur = nxt
	}
	return true
}

// Walk 從 (sel, off) 開始往後走 n 個 byte，跨 huge 段時自動換 selector。
// fn 拿到的是每一段的切片；回 false 就停。
func (m *Memory) Walk(sel, off uint16, n int, fn func(part []byte) bool) int {
	done := 0
	for n > 0 {
		b, ok := m.blocks[sel]
		if !ok || int(off) >= len(b.Data) {
			return done
		}
		part := b.Data[off:]
		if len(part) > n {
			part = part[:n]
		}
		if !fn(part) {
			return done + len(part)
		}
		done += len(part)
		n -= len(part)
		sel, off = b.HugeNext, 0
		if sel == 0 {
			return done
		}
	}
	return done
}

// SelectorLimit 實作 cpu.SelectorInfo：回傳最後一個合法位移。
//
// 界限用「進位到 32 個 byte 之後的大小減一」——和 Win16 全域堆積的
// 配置粒度一致（見 loader.go 的段配置）。
func (m *Memory) SelectorLimit(sel uint16) (uint32, bool) {
	b, ok := m.blocks[sel]
	if !ok {
		return 0, false
	}
	return uint32(len(b.Data)) - 1, true
}

// Free 拿掉一個 selector。已經釋放的 selector 再讀寫會回
// SelInvalidError——這是刻意的：懸空指標要在第一次使用時就炸，
// 不能安靜地讀到舊內容。
func (m *Memory) Free(sel uint16) bool {
	if _, ok := m.blocks[sel]; !ok {
		return false
	}
	delete(m.blocks, sel)
	return true
}

// Resize 換一塊新的大小，內容照舊（截短或補零），selector 不變。
func (m *Memory) Resize(sel uint16, size int) bool {
	b, ok := m.blocks[sel]
	if !ok {
		return false
	}
	data := make([]byte, size)
	copy(data, b.Data)
	b.Data = data
	return true
}
