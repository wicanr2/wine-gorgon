package win16

// CPUBus 讓 CPU 用 32 位元位移看記憶體。
//
// Memory 對 win16 這一側的介面維持 16 位元位移（呼叫點有上百處，而且
// 那些地方談的都是 64 KiB 以內的結構）。CPU 需要的是另一件事：386 的
// 段界可以超過 64 KiB，PTO2 就用 EDI 直接走完整張 WinG DIB。兩個需求
// 分兩個入口，比把所有呼叫點都改寬誠實——**界限仍然是 Block 的長度，
// 這裡沒有放寬任何檢查**，只是不再把位移截成 16 位元。
type CPUBus struct{ m *Memory }

// NewCPUBus 包一份 Memory 給 CPU 用。
func NewCPUBus(m *Memory) *CPUBus { return &CPUBus{m: m} }

func (b *CPUBus) ReadU8(sel uint16, off uint32) (uint8, error) {
	blk, err := b.m.bounds(sel, int(off), 1, "讀 byte")
	if err != nil {
		return 0, err
	}
	return blk.Data[off], nil
}

func (b *CPUBus) ReadU16(sel uint16, off uint32) (uint16, error) {
	blk, err := b.m.bounds(sel, int(off), 2, "讀 word")
	if err != nil {
		return 0, err
	}
	return uint16(blk.Data[off]) | uint16(blk.Data[off+1])<<8, nil
}

func (b *CPUBus) WriteU8(sel uint16, off uint32, v uint8) error {
	blk, err := b.m.bounds(sel, int(off), 1, "寫 byte")
	if err != nil {
		return err
	}
	blk.Data[off] = v
	return nil
}

func (b *CPUBus) WriteU16(sel uint16, off uint32, v uint16) error {
	blk, err := b.m.bounds(sel, int(off), 2, "寫 word")
	if err != nil {
		return err
	}
	blk.Data[off] = uint8(v)
	blk.Data[off+1] = uint8(v >> 8)
	return nil
}

// SelectorLimit 讓 286 的 VERR／VERW／LSL 照樣問得到。
func (b *CPUBus) SelectorLimit(sel uint16) (uint32, bool) { return b.m.SelectorLimit(sel) }
