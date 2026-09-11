package cpu

import "testing"

// TestAddr32BeyondSegment64K 釘住「段界超過 64 KiB」這件事。
//
// PTO2 的 1bpp→8bpp 展開迴圈用 `67` 前綴拿 EDI 走完整張 640×416 的
// WinG DIB（`RE:cseg11:0x54d4`）。把位移截成 16 位元的話，第 65,536 個
// 位元組之後會繞回段頭——畫面看起來只是「花了」，不會報錯。
func TestAddr32BeyondSegment64K(t *testing.T) {
	bus := newBus()
	bus.seg[tES] = make([]byte, 0x20000) // 128 KiB，就是大於 64 KiB 的段

	// 67 26 88 1F   mov es:[edi], bl
	// 67 AC         lodsb（用 ESI）
	// F4            hlt
	code := []byte{0x67, 0x26, 0x88, 0x1F, 0x67, 0xAC, 0xF4}
	copy(bus.seg[tCS], code)
	c := New(bus)
	c.Seg[CS], c.Seg[DS], c.Seg[SS], c.Seg[ES] = tCS, tDS, tSS, tES
	c.R[DI] = 0x000101B1 // 執行時實際量到的那個值
	c.R[SI] = 0x00000010
	c.SetReg8(3, 0xAB) // BL
	bus.seg[tDS][0x10] = 0x5A

	if err := c.Run(10); err != nil {
		t.Fatalf("跑不完：%v", err)
	}
	if got := bus.seg[tES][0x101B1]; got != 0xAB {
		t.Errorf("ES:[EDI] 寫到 0x101B1 的值是 %02X，要 AB", got)
	}
	// lodsb 走的是 ESI，而且要整個 32 位元加一。
	if got := c.Reg8(0); got != 0x5A {
		t.Errorf("AL = %02X，要 5A", got)
	}
	if c.R[SI] != 0x11 {
		t.Errorf("ESI = %08X，要 00000011", c.R[SI])
	}
}

// TestAddr32IndexKeepsHighHalf 釘住索引暫存器的高半部不會被丟掉。
func TestAddr32IndexKeepsHighHalf(t *testing.T) {
	bus := newBus()
	bus.seg[tES] = make([]byte, 0x20000)
	// 67 F3 AA   rep stosb（ESI/EDI/ECX 都是 32 位元）
	code := []byte{0x67, 0xF3, 0xAA, 0xF4}
	copy(bus.seg[tCS], code)
	c := New(bus)
	c.Seg[CS], c.Seg[DS], c.Seg[SS], c.Seg[ES] = tCS, tDS, tSS, tES
	c.R[DI] = 0xFFFE
	c.R[CX] = 4
	c.SetReg8(0, 0x7E) // AL

	if err := c.Run(10); err != nil {
		t.Fatalf("跑不完：%v", err)
	}
	for i, want := range map[int]uint8{0xFFFE: 0x7E, 0xFFFF: 0x7E, 0x10000: 0x7E, 0x10001: 0x7E} {
		if got := bus.seg[tES][i]; got != want {
			t.Errorf("ES:[%X] = %02X，要 %02X（跨過 64 KiB 沒有繞回段頭）", i, got, want)
		}
	}
	if c.R[DI] != 0x10002 {
		t.Errorf("EDI = %08X，要 00010002", c.R[DI])
	}
}
