// Package cpu 是 x86 的 16 位元執行核心（8086／80186／80286 的應用層指令，
// 加上 386 的運算元大小前綴與 32 位元暫存器）。
//
// 它不做保護模式：段暫存器裡的 selector 直接交給 Bus 查表
// （`docs/spec/001` §3）。Win16 應用程式看不到 descriptor 的內容，所以
// 「selector ＋ 位移」對它而言就是完整的位址。
//
// 位寬是**參數**不是分支：每條指令帶一個 Size（1／2／4 個 byte），
// `66` 前綴只是把預設的 2 換成 4。這樣 386 的 32 位元運算不需要另一套
// 指令表，之後接別的老遊戲要往上補也只是多幾個 case。
//
// 設計上刻意讓「未實作的 opcode」是一個**帶位址的錯誤**而不是靜靜跳過：
// 接一支新程式時最貴的不是實作指令，是找出它到底停在哪裡。
package cpu

import "fmt"

// Bus 是 CPU 看得到的記憶體。抽成介面是為了讓指令測試不必先鋪一個 NE。
type Bus interface {
	ReadU8(sel, off uint16) (uint8, error)
	ReadU16(sel, off uint16) (uint16, error)
	WriteU8(sel, off uint16, v uint8) error
	WriteU16(sel, off uint16, v uint16) error
}

// SelectorInfo 是 Bus 可以額外提供的 selector 資訊。
//
// 286 的 `VERR`／`VERW`／`LSL` 只需要知道「這個 selector 有沒有配置、
// 它的界限是多少」——在 selector 就是 handle 的模型下（spec 001 §3），
// 這兩個問題都有明確答案，不必真的做 descriptor。
type SelectorInfo interface {
	// SelectorLimit 回傳最後一個合法位移；ok 為 false 表示沒有這個 selector。
	SelectorLimit(sel uint16) (limit uint32, ok bool)
}

// Size 是運算元位寬，單位是 byte。
type Size int

// 三種位寬。
const (
	S8  Size = 1
	S16 Size = 2
	S32 Size = 4
)

// 通用暫存器索引（就是 ModRM 的 reg 欄編碼）。
const (
	AX = iota
	CX
	DX
	BX
	SP
	BP
	SI
	DI
)

// 段暫存器索引（也是 ModRM 的編碼）。
const (
	ES = iota
	CS
	SS
	DS
)

// 旗標位元。
const (
	FlagCF = 1 << 0
	FlagPF = 1 << 2
	FlagAF = 1 << 4
	FlagZF = 1 << 6
	FlagSF = 1 << 7
	FlagTF = 1 << 8
	FlagIF = 1 << 9
	FlagDF = 1 << 10
	FlagOF = 1 << 11
)

// CPU 是一顆執行核心。
type CPU struct {
	// R 是八個 32 位元通用暫存器（EAX…EDI）。16 位元程式只用低半部，
	// 但 `66` 前綴的指令會動到高半部，所以整個存起來。
	R     [8]uint32
	Seg   [4]uint16
	IP    uint16
	Flags uint16
	Bus   Bus

	// OnFarCall 在 far call／far jmp 進入某個 selector 之前被呼叫。
	// 回 `true` 表示「這一跳已經被接手了」——載入器用它攔 API thunk。
	OnFarCall func(c *CPU, sel, off uint16) (handled bool, err error)

	// OnInt 服務軟體中斷。Win16 程式主要走 API，但 Borland 的啟動碼還是
	// 會用 INT 1Ah 取時間、INT 21h 取 DOS 版本與結束行程。
	// 回 `false` 表示沒人處理，CPU 會以「未實作的軟體中斷」停下。
	OnInt func(c *CPU, n uint8) (handled bool, err error)

	Steps uint64
	Halt  bool

	// 一條指令的解碼狀態
	segOverride int // -1 表示沒有
	repPrefix   uint8
	opSize      Size // 預設 S16，被 66 前綴改成 S32
}

// New 造一顆重置狀態的 CPU。
func New(bus Bus) *CPU {
	return &CPU{Bus: bus, Flags: 0x0002, segOverride: -1, opSize: S16}
}

// R16 讀通用暫存器的低 16 位。
func (c *CPU) R16(i int) uint16 { return uint16(c.R[i]) }

// SetR16 只寫低 16 位；高半部保留——這是 386 的行為，不是疏忽。
func (c *CPU) SetR16(i int, v uint16) { c.R[i] = c.R[i]&0xFFFF0000 | uint32(v) }

// Reg8 讀 8 位元暫存器（ModRM 編碼：AL CL DL BL AH CH DH BH）。
func (c *CPU) Reg8(i int) uint8 {
	if i < 4 {
		return uint8(c.R[i])
	}
	return uint8(c.R[i-4] >> 8)
}

// SetReg8 寫 8 位元暫存器。
func (c *CPU) SetReg8(i int, v uint8) {
	if i < 4 {
		c.R[i] = c.R[i]&0xFFFFFF00 | uint32(v)
		return
	}
	c.R[i-4] = c.R[i-4]&0xFFFF00FF | uint32(v)<<8
}

// reg 依位寬讀暫存器。
func (c *CPU) reg(i int, sz Size) uint32 {
	switch sz {
	case S8:
		return uint32(c.Reg8(i))
	case S32:
		return c.R[i]
	default:
		return uint32(uint16(c.R[i]))
	}
}

// setReg 依位寬寫暫存器。
func (c *CPU) setReg(i int, v uint32, sz Size) {
	switch sz {
	case S8:
		c.SetReg8(i, uint8(v))
	case S32:
		c.R[i] = v
	default:
		c.SetR16(i, uint16(v))
	}
}

// Flag 讀一個旗標。
func (c *CPU) Flag(f uint16) bool { return c.Flags&f != 0 }

// SetFlag 設一個旗標。
func (c *CPU) SetFlag(f uint16, on bool) {
	if on {
		c.Flags |= f
	} else {
		c.Flags &^= f
	}
}

// Error 是帶執行位置的錯誤。**所有 CPU 錯誤都要經過它**——沒有位址的
// 「未實作」訊息會讓下一個人重頭找一次。
type Error struct {
	CS, IP uint16
	Steps  uint64
	Msg    string
	Cause  error
}

func (e *Error) Error() string {
	s := fmt.Sprintf("cpu: %04X:%04X（第 %d 步）%s", e.CS, e.IP, e.Steps, e.Msg)
	if e.Cause != nil {
		s += ": " + e.Cause.Error()
	}
	return s
}

func (e *Error) Unwrap() error { return e.Cause }

func (c *CPU) errf(ip uint16, format string, a ...any) error {
	return &Error{CS: c.Seg[CS], IP: ip, Steps: c.Steps, Msg: fmt.Sprintf(format, a...)}
}

func (c *CPU) wrap(ip uint16, err error, format string, a ...any) error {
	if err == nil {
		return nil
	}
	return &Error{CS: c.Seg[CS], IP: ip, Steps: c.Steps, Msg: fmt.Sprintf(format, a...), Cause: err}
}

// --- 匯流排：32 位元讀寫拆成兩次 16 位元，越界照樣是錯誤 ---

func (c *CPU) busRead(sel, off uint16, sz Size) (uint32, error) {
	switch sz {
	case S8:
		v, err := c.Bus.ReadU8(sel, off)
		return uint32(v), err
	case S32:
		lo, err := c.Bus.ReadU16(sel, off)
		if err != nil {
			return 0, err
		}
		hi, err := c.Bus.ReadU16(sel, off+2)
		return uint32(hi)<<16 | uint32(lo), err
	default:
		v, err := c.Bus.ReadU16(sel, off)
		return uint32(v), err
	}
}

func (c *CPU) busWrite(sel, off uint16, sz Size, v uint32) error {
	switch sz {
	case S8:
		return c.Bus.WriteU8(sel, off, uint8(v))
	case S32:
		if err := c.Bus.WriteU16(sel, off, uint16(v)); err != nil {
			return err
		}
		return c.Bus.WriteU16(sel, off+2, uint16(v>>16))
	default:
		return c.Bus.WriteU16(sel, off, uint16(v))
	}
}

// --- 取指 ---

func (c *CPU) fetch8() (uint8, error) {
	v, err := c.Bus.ReadU8(c.Seg[CS], c.IP)
	if err != nil {
		return 0, err
	}
	c.IP++
	return v, nil
}

func (c *CPU) fetch16() (uint16, error) {
	v, err := c.Bus.ReadU16(c.Seg[CS], c.IP)
	if err != nil {
		return 0, err
	}
	c.IP += 2
	return v, nil
}

func (c *CPU) fetch32() (uint32, error) {
	lo, err := c.fetch16()
	if err != nil {
		return 0, err
	}
	hi, err := c.fetch16()
	return uint32(hi)<<16 | uint32(lo), err
}

// fetchImm 取一個和運算元同寬的立即數（8 位元運算元配 8 位元立即數）。
func (c *CPU) fetchImm(sz Size) (uint32, error) {
	switch sz {
	case S8:
		v, err := c.fetch8()
		return uint32(v), err
	case S32:
		return c.fetch32()
	default:
		v, err := c.fetch16()
		return uint32(v), err
	}
}

// --- 堆疊 ---
//
// 堆疊寬度跟著運算元大小走：`66 58` 是 `pop eax`，會彈 4 個 byte。
// SP 一律是 16 位元（段是 16 位元的）。

func (c *CPU) pushSize(v uint32, sz Size) error {
	if sz == S32 {
		c.SetR16(SP, c.R16(SP)-4)
		return c.busWrite(c.Seg[SS], c.R16(SP), S32, v)
	}
	c.SetR16(SP, c.R16(SP)-2)
	return c.Bus.WriteU16(c.Seg[SS], c.R16(SP), uint16(v))
}

func (c *CPU) popSize(sz Size) (uint32, error) {
	v, err := c.busRead(c.Seg[SS], c.R16(SP), sz)
	if err != nil {
		return 0, err
	}
	if sz == S32 {
		c.SetR16(SP, c.R16(SP)+4)
	} else {
		c.SetR16(SP, c.R16(SP)+2)
	}
	return v, nil
}

func (c *CPU) push16(v uint16) error { return c.pushSize(uint32(v), S16) }

func (c *CPU) pop16() (uint16, error) {
	v, err := c.popSize(S16)
	return uint16(v), err
}

// dataSeg 回傳這條指令實際要用的資料段（考慮覆寫前綴）。
func (c *CPU) dataSeg(def int) uint16 {
	if c.segOverride >= 0 {
		return c.Seg[c.segOverride]
	}
	return c.Seg[def]
}

// PushWord 推一個 16 位元值。給外部（win16 的回呼機制）鋪參數用。
func (c *CPU) PushWord(v uint16) error { return c.push16(v) }

// PopWord 彈一個 16 位元值。
func (c *CPU) PopWord() (uint16, error) { return c.pop16() }
