// Package cpu 是 16 位元 x86 核心（8086／80186／80286 的**應用層**指令）。
//
// 它不做保護模式：段暫存器裡的 selector 直接交給 Bus 查表
// （`docs/spec/001` §3）。Win16 應用程式看不到 descriptor 的內容，所以
// 「selector ＋ 位移」對它而言就是完整的位址。
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

// CPU 是一顆 16 位元核心。
type CPU struct {
	R     [8]uint16
	Seg   [4]uint16
	IP    uint16
	Flags uint16
	Bus   Bus

	// OnFarCall 在 far call／far jmp 進入某個 selector 之前被呼叫。
	// 回 `true` 表示「這一跳已經被接手了」——載入器用它攔 API thunk。
	OnFarCall func(c *CPU, sel, off uint16) (handled bool, err error)

	Steps uint64
	Halt  bool

	// 一條指令的解碼狀態
	segOverride int // -1 表示沒有
	repPrefix   uint8
}

// New 造一顆重置狀態的 CPU。
func New(bus Bus) *CPU {
	return &CPU{Bus: bus, Flags: 0x0002, segOverride: -1}
}

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
		c.R[i] = c.R[i]&0xFF00 | uint16(v)
		return
	}
	c.R[i-4] = c.R[i-4]&0x00FF | uint16(v)<<8
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

// --- 堆疊 ---

func (c *CPU) push16(v uint16) error {
	c.R[SP] -= 2
	return c.Bus.WriteU16(c.Seg[SS], c.R[SP], v)
}

func (c *CPU) pop16() (uint16, error) {
	v, err := c.Bus.ReadU16(c.Seg[SS], c.R[SP])
	if err != nil {
		return 0, err
	}
	c.R[SP] += 2
	return v, nil
}

// dataSeg 回傳這條指令實際要用的資料段（考慮覆寫前綴）。
func (c *CPU) dataSeg(def int) uint16 {
	if c.segOverride >= 0 {
		return c.Seg[c.segOverride]
	}
	return c.Seg[def]
}
