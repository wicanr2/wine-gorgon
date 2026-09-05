package cpu

import (
	"errors"
	"strings"
	"testing"
)

// flatBus 是測試用的位址空間：幾個固定 selector，各 64 KiB。
// 刻意不用 win16.Memory——指令的正確性不該依賴載入器。
type flatBus struct{ seg map[uint16][]byte }

const (
	tCS = 0x000F
	tDS = 0x001F
	tSS = 0x0017
	tES = 0x0027
)

func newBus() *flatBus {
	b := &flatBus{seg: map[uint16][]byte{}}
	for _, s := range []uint16{tCS, tDS, tSS, tES} {
		b.seg[s] = make([]byte, 0x10000)
	}
	return b
}

var errNoSeg = errors.New("沒有這個 selector")

func (b *flatBus) at(sel uint16) ([]byte, error) {
	m, ok := b.seg[sel]
	if !ok {
		return nil, errNoSeg
	}
	return m, nil
}

func (b *flatBus) ReadU8(sel, off uint16) (uint8, error) {
	m, err := b.at(sel)
	return orZero(m, off), err
}

func (b *flatBus) ReadU16(sel, off uint16) (uint16, error) {
	m, err := b.at(sel)
	if err != nil {
		return 0, err
	}
	return uint16(m[off]) | uint16(m[uint16(off+1)])<<8, nil
}

func (b *flatBus) WriteU8(sel, off uint16, v uint8) error {
	m, err := b.at(sel)
	if err != nil {
		return err
	}
	m[off] = v
	return nil
}

func (b *flatBus) WriteU16(sel, off uint16, v uint16) error {
	m, err := b.at(sel)
	if err != nil {
		return err
	}
	m[off] = uint8(v)
	m[uint16(off+1)] = uint8(v >> 8)
	return nil
}

func orZero(m []byte, off uint16) uint8 {
	if m == nil {
		return 0
	}
	return m[off]
}

// run 把 code 放在 tCS:0000 跑 steps 條指令。
func run(t *testing.T, code []byte, steps int, setup func(*CPU, *flatBus)) (*CPU, *flatBus) {
	t.Helper()
	bus := newBus()
	copy(bus.seg[tCS], code)
	c := New(bus)
	c.Seg[CS], c.Seg[DS], c.Seg[SS], c.Seg[ES] = tCS, tDS, tSS, tES
	c.SetR16(SP, 0xFFFE)
	if setup != nil {
		setup(c, bus)
	}
	for i := 0; i < steps; i++ {
		if err := c.Step(); err != nil {
			t.Fatalf("第 %d 步失敗：%v", i, err)
		}
	}
	return c, bus
}

func TestArithmeticAndFlags(t *testing.T) {
	cases := []struct {
		name  string
		code  []byte
		steps int
		want  func(*testing.T, *CPU)
	}{
		{
			// 加到剛好進位歸零：CF 與 ZF 要同時成立。
			name:  "add 進位歸零",
			code:  []byte{0xB8, 0x34, 0x12, 0x05, 0xCC, 0xED},
			steps: 2,
			want: func(t *testing.T, c *CPU) {
				eq16(t, "AX", c.R16(AX), 0)
				flag(t, c, FlagCF, true)
				flag(t, c, FlagZF, true)
			},
		},
		{
			// 0x80 - 1：無號沒有借位，但有號從最小值掉出去 ⇒ OF=1、SF=0。
			name:  "sub 有號溢位",
			code:  []byte{0xB0, 0x80, 0x2C, 0x01},
			steps: 2,
			want: func(t *testing.T, c *CPU) {
				eq16(t, "AL", uint16(c.Reg8(0)), 0x7F)
				flag(t, c, FlagOF, true)
				flag(t, c, FlagSF, false)
				flag(t, c, FlagCF, false)
			},
		},
		{
			name:  "sar 保留符號",
			code:  []byte{0xB8, 0x01, 0x80, 0xD1, 0xF8},
			steps: 2,
			want: func(t *testing.T, c *CPU) {
				eq16(t, "AX", c.R16(AX), 0xC000)
				flag(t, c, FlagCF, true)
			},
		},
		{
			name:  "rol 8 位元",
			code:  []byte{0xB0, 0x81, 0xD0, 0xC0},
			steps: 2,
			want: func(t *testing.T, c *CPU) {
				eq16(t, "AL", uint16(c.Reg8(0)), 0x03)
				flag(t, c, FlagCF, true)
			},
		},
		{
			name:  "mul 進到 DX",
			code:  []byte{0xB8, 0x2C, 0x01, 0xBB, 0x07, 0x00, 0xF7, 0xE3},
			steps: 3,
			want: func(t *testing.T, c *CPU) {
				eq16(t, "AX", c.R16(AX), 2100)
				eq16(t, "DX", c.R16(DX), 0)
				flag(t, c, FlagCF, false)
			},
		},
		{
			// -10 / 3 = -3 餘 -1：x86 的餘數跟被除數同號，和 Go 一致。
			name:  "idiv 負數",
			code:  []byte{0xB8, 0xF6, 0xFF, 0x99, 0xBB, 0x03, 0x00, 0xF7, 0xFB},
			steps: 4,
			want: func(t *testing.T, c *CPU) {
				eq16(t, "AX", c.R16(AX), 0xFFFD)
				eq16(t, "DX", c.R16(DX), 0xFFFF)
			},
		},
		{
			// mov cx,5 / xor ax,ax / (add ax,cx / loop) → 15
			name:  "loop 累加",
			code:  []byte{0xB9, 0x05, 0x00, 0x31, 0xC0, 0x01, 0xC8, 0xE2, 0xFC},
			steps: 2 + 5*2,
			want: func(t *testing.T, c *CPU) {
				eq16(t, "AX", c.R16(AX), 15)
				eq16(t, "CX", c.R16(CX), 0)
			},
		},
		{
			// 0x83 的立即數是符號延伸：add ax, -2 而不是 add ax, 0xFE。
			name:  "0x83 符號延伸",
			code:  []byte{0xB8, 0x10, 0x00, 0x83, 0xC0, 0xFE},
			steps: 2,
			want:  func(t *testing.T, c *CPU) { eq16(t, "AX", c.R16(AX), 14) },
		},
		{
			// inc 不動 CF：先用 stc 立起來，inc 之後要還在。
			name:  "inc 不動 CF",
			code:  []byte{0xF9, 0xB8, 0x01, 0x00, 0x40},
			steps: 3,
			want: func(t *testing.T, c *CPU) {
				eq16(t, "AX", c.R16(AX), 2)
				flag(t, c, FlagCF, true)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := run(t, tc.code, tc.steps, nil)
			tc.want(t, c)
		})
	}
}

// 用到 BP 的定址預設走 SS。寫錯的話不會當掉，只會安靜地讀寫錯的段——
// 所以要正反兩面都驗：SS 有值，DS 同一個位移要是空的。
func TestBPAddressingDefaultsToStack(t *testing.T) {
	// mov bp, 0x100 / mov word [bp+4], 0xBEEF
	code := []byte{0xBD, 0x00, 0x01, 0xC7, 0x46, 0x04, 0xEF, 0xBE}
	_, bus := run(t, code, 2, nil)
	if got := le16(bus.seg[tSS], 0x104); got != 0xBEEF {
		t.Errorf("SS:0104 = %04X，預期 BEEF", got)
	}
	if got := le16(bus.seg[tDS], 0x104); got != 0 {
		t.Errorf("DS:0104 = %04X，預期 0（[bp] 不該走資料段）", got)
	}
}

// 段覆寫前綴要壓過預設段。
func TestSegmentOverride(t *testing.T) {
	// mov bx, 0x200 / mov es:[bx], ax
	code := []byte{0xBB, 0x00, 0x02, 0x26, 0x89, 0x07}
	_, bus := run(t, code, 2, func(c *CPU, _ *flatBus) { c.SetR16(AX, 0x1234) })
	if got := le16(bus.seg[tES], 0x200); got != 0x1234 {
		t.Errorf("ES:0200 = %04X，預期 1234", got)
	}
	if got := le16(bus.seg[tDS], 0x200); got != 0 {
		t.Errorf("DS:0200 = %04X，預期 0", got)
	}
}

func TestRepMovsw(t *testing.T) {
	// cld / rep movsw
	code := []byte{0xFC, 0xF3, 0xA5}
	c, bus := run(t, code, 2, func(c *CPU, bus *flatBus) {
		c.SetR16(SI, 0x10)
		c.SetR16(DI, 0x40)
		c.SetR16(CX, 4)
		for i := 0; i < 8; i++ {
			bus.seg[tDS][0x10+i] = byte(0xA0 + i)
		}
	})
	for i := 0; i < 8; i++ {
		if got := bus.seg[tES][0x40+i]; got != byte(0xA0+i) {
			t.Fatalf("ES:%04X = %02X，預期 %02X", 0x40+i, got, 0xA0+i)
		}
	}
	eq16(t, "CX", c.R16(CX), 0)
	eq16(t, "SI", c.R16(SI), 0x18)
	eq16(t, "DI", c.R16(DI), 0x48)
}

// REPE CMPSB 要在第一個不同的位元組停下，而且 CX 反映的是「還沒比的個數」。
func TestRepeCmpsbStopsAtDifference(t *testing.T) {
	code := []byte{0xFC, 0xF3, 0xA6}
	c, _ := run(t, code, 2, func(c *CPU, bus *flatBus) {
		c.SetR16(SI, 0)
		c.SetR16(DI, 0)
		c.SetR16(CX, 8)
		copy(bus.seg[tDS], []byte("abcdefgh"))
		copy(bus.seg[tES], []byte("abcXefgh"))
	})
	eq16(t, "CX", c.R16(CX), 4)
	flag(t, c, FlagZF, false)
}

// far call 要能被攔下來：處理器看得到目標，也能自己 retf 回去。
func TestFarCallHookAndRetFar(t *testing.T) {
	// call far 0xF00F:0x0020 / mov bx, 0x77
	code := []byte{0x9A, 0x20, 0x00, 0x0F, 0xF0, 0xBB, 0x77, 0x00}
	bus := newBus()
	copy(bus.seg[tCS], code)
	c := New(bus)
	c.Seg[CS], c.Seg[DS], c.Seg[SS], c.Seg[ES] = tCS, tDS, tSS, tES
	c.SetR16(SP, 0x1000)

	var gotSel, gotOff uint16
	calls := 0
	c.OnFarCall = func(c *CPU, sel, off uint16) (bool, error) {
		calls++
		gotSel, gotOff = sel, off
		c.SetR16(AX, 0x4242)
		return true, c.RetFar(4) // 假裝是一支吃 4 個位元組參數的 pascal API
	}
	if err := c.Step(); err != nil {
		t.Fatalf("far call：%v", err)
	}
	if calls != 1 || gotSel != 0xF00F || gotOff != 0x20 {
		t.Fatalf("攔截到 %d 次 %04X:%04X，預期 1 次 F00F:0020", calls, gotSel, gotOff)
	}
	eq16(t, "CS", c.Seg[CS], tCS)
	eq16(t, "IP", c.IP, 5)
	eq16(t, "SP", c.R16(SP), 0x1004)
	if err := c.Step(); err != nil {
		t.Fatalf("回來之後：%v", err)
	}
	eq16(t, "BX", c.R16(BX), 0x77)
	eq16(t, "AX", c.R16(AX), 0x4242)
}

// 未實作的 opcode 必須講出位址——不然接一支新程式時最貴的是找位置。
func TestUnimplementedOpcodeNamesAddress(t *testing.T) {
	bus := newBus()
	bus.seg[tCS][0x10] = 0x64 // FS 前綴，386 才有
	c := New(bus)
	c.Seg[CS], c.IP = tCS, 0x10
	err := c.Step()
	if err == nil {
		t.Fatal("預期錯誤，卻沒有")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("錯誤型別是 %T，預期 *cpu.Error", err)
	}
	if ce.IP != 0x10 || ce.CS != tCS {
		t.Errorf("錯誤位址 %04X:%04X，預期 000F:0010", ce.CS, ce.IP)
	}
	if !strings.Contains(err.Error(), "64") {
		t.Errorf("錯誤訊息沒提到 opcode：%v", err)
	}
}

func TestEnterLeave(t *testing.T) {
	// enter 6,0 / leave
	code := []byte{0xC8, 0x06, 0x00, 0x00, 0xC9}
	c, _ := run(t, code, 1, func(c *CPU, _ *flatBus) { c.SetR16(BP, 0xAAAA) })
	eq16(t, "SP", c.R16(SP), 0xFFFE-2-6)
	eq16(t, "BP", c.R16(BP), 0xFFFE-2)
	if err := c.Step(); err != nil {
		t.Fatalf("leave：%v", err)
	}
	eq16(t, "SP", c.R16(SP), 0xFFFE)
	eq16(t, "BP", c.R16(BP), 0xAAAA)
}

func TestLesLoadsBothHalves(t *testing.T) {
	// les di, [bx]
	code := []byte{0xC4, 0x3F}
	c, _ := run(t, code, 1, func(c *CPU, bus *flatBus) {
		c.SetR16(BX, 0x30)
		_ = bus.WriteU16(tDS, 0x30, 0x1234)
		_ = bus.WriteU16(tDS, 0x32, tES)
	})
	eq16(t, "DI", c.R16(DI), 0x1234)
	eq16(t, "ES", c.Seg[ES], tES)
}

func eq16(t *testing.T, name string, got, want uint16) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %04X，預期 %04X", name, got, want)
	}
}

func flag(t *testing.T, c *CPU, f uint16, want bool) {
	t.Helper()
	if c.Flag(f) != want {
		t.Errorf("旗標 %04X = %v，預期 %v", f, c.Flag(f), want)
	}
}

func le16(m []byte, off int) uint16 { return uint16(m[off]) | uint16(m[off+1])<<8 }

// 386 的 66 前綴：位寬是參數，不是另一套指令表。
func Test386OperandSizePrefix(t *testing.T) {
	cases := []struct {
		name  string
		code  []byte
		steps int
		want  func(*testing.T, *CPU)
	}{
		{
			// push dx / push ax / pop eax：把 DX:AX 併成 EAX。
			// CIV.EXE 的 000F… 記憶體檢查就是這個寫法。
			name:  "pop eax 併 DX:AX",
			code:  []byte{0x52, 0x50, 0x66, 0x58},
			steps: 3,
			want: func(t *testing.T, c *CPU) {
				if c.R[AX] != 0x00401234 {
					t.Errorf("EAX = %08X，預期 00401234", c.R[AX])
				}
			},
		},
		{
			// 寫 16 位元不動高半部——這是 386 的行為，不是疏忽。
			name:  "寫 AX 保留 EAX 高半部",
			code:  []byte{0xB8, 0xEF, 0xBE},
			steps: 1,
			want: func(t *testing.T, c *CPU) {
				if c.R[AX] != 0xDEADBEEF {
					t.Errorf("EAX = %08X，預期 DEADBEEF", c.R[AX])
				}
			},
		},
		{
			// cmp dword [0100], 002625A0h：32 位元立即數比較。
			name:  "32 位元 cmp",
			code:  []byte{0x66, 0x81, 0x3E, 0x00, 0x01, 0xA0, 0x25, 0x26, 0x00},
			steps: 1,
			want: func(t *testing.T, c *CPU) {
				flag(t, c, FlagCF, true) // 記憶體是 0，比 2,500,000 小
				flag(t, c, FlagZF, false)
			},
		},
		{
			name:  "movzx 與 movsx",
			code:  []byte{0xB0, 0x80, 0x0F, 0xB6, 0xD8, 0x0F, 0xBE, 0xC8},
			steps: 3,
			want: func(t *testing.T, c *CPU) {
				eq16(t, "BX", c.R16(BX), 0x0080)
				eq16(t, "CX", c.R16(CX), 0xFF80)
			},
		},
		{
			// 0F AF：雙運算元 IMUL。
			name:  "imul r16, r/m16",
			code:  []byte{0xB8, 0x0A, 0x00, 0xBB, 0xF6, 0xFF, 0x0F, 0xAF, 0xC3},
			steps: 3,
			want:  func(t *testing.T, c *CPU) { eq16(t, "AX", c.R16(AX), 0xFF9C) }, // -100
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := run(t, tc.code, tc.steps, func(c *CPU, _ *flatBus) {
				c.R[AX] = 0xDEAD0000
				c.SetR16(AX, 0x1234)
				c.SetR16(DX, 0x0040)
			})
			tc.want(t, c)
		})
	}
}

// 32 位元除法：EDX:EAX / r/m32。
func Test32BitDivide(t *testing.T) {
	// mov eax, 0x000F4240 (1000000) / xor edx,edx / mov ebx, 7 / div ebx
	code := []byte{
		0x66, 0xB8, 0x40, 0x42, 0x0F, 0x00,
		0x66, 0x31, 0xD2,
		0x66, 0xBB, 0x07, 0x00, 0x00, 0x00,
		0x66, 0xF7, 0xF3,
	}
	c, _ := run(t, code, 4, nil)
	if c.R[AX] != 142857 || c.R[DX] != 1 {
		t.Errorf("EAX=%d EDX=%d，預期 142857 餘 1", c.R[AX], c.R[DX])
	}
}
