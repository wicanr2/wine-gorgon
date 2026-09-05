package win16

import (
	"fmt"
	"time"

	"github.com/wicanr2/wine-gorgon/internal/cpu"
)

// Borland 的 Win16 啟動碼在進 Windows API 之前還會用三個 DOS／BIOS
// 中斷。它們不是 Windows 的一部分，但不接就跑不到 `WinMain`。
//
// 時間一律走 Process.Clock，**不讀主機時鐘**：對拍工具的每一次執行都要
// 能重現，而「現在幾點」是最容易讓兩次執行分岔的東西。

// Clock 提供可重現的時間。
type Clock interface {
	// Millis 是從行程開始算起的毫秒數。
	Millis() uint32
}

// FixedClock 讓時間停在一個值上；再跑一次結果一樣。
type FixedClock struct{ Value uint32 }

// Millis 實作 Clock。
func (c *FixedClock) Millis() uint32 { return c.Value }

// StepClock 讓時間隨執行步數前進：不是真時間，但單調、可重現，
// 而且會讓「等一段時間」的迴圈真的走得完。
type StepClock struct {
	CPU     *cpu.CPU
	PerStep uint32 // 每條指令算幾微秒
}

// Millis 實作 Clock。
func (c *StepClock) Millis() uint32 {
	if c.PerStep == 0 {
		c.PerStep = 10 // 約當 100k 指令／秒
	}
	return uint32(c.CPU.Steps * uint64(c.PerStep) / 1000)
}

// WallClock 走主機時鐘。**只有互動除錯時才該用**：它會讓兩次執行不同。
type WallClock struct{ start time.Time }

// Millis 實作 Clock。
func (c *WallClock) Millis() uint32 {
	if c.start.IsZero() {
		c.start = time.Now()
	}
	return uint32(time.Since(c.start).Milliseconds())
}

// ExitError 是程式自己結束（INT 21h AH=4Ch）。
type ExitError struct{ Code uint8 }

func (e *ExitError) Error() string {
	return fmt.Sprintf("win16: 程式呼叫 INT 21h AH=4Ch 結束，離開碼 %d", e.Code)
}

// onInt 服務 Borland 啟動碼用得到的那幾個中斷。
func (p *Process) onInt(c *cpu.CPU, n uint8) (bool, error) {
	ah := uint8(c.R[cpu.AX] >> 8)
	switch n {
	case 0x1A: // BIOS 即時時鐘
		if ah != 0 {
			return false, nil
		}
		// AH=00：CX:DX ＝ 開機以來的計時器格數（每格 1/18.2 秒），
		// AL ＝ 是否跨過午夜。回 0 就不會走進「改 BIOS 資料區」那條路。
		ticks := uint32(float64(p.Clock.Millis()) / 54.925)
		c.SetR16(cpu.CX, uint16(ticks>>16))
		c.SetR16(cpu.DX, uint16(ticks))
		c.SetReg8(0, 0)
		return true, nil
	case 0x21:
		switch ah {
		case 0x19: // 取目前磁碟機（0=A）
			c.SetReg8(0, p.CurrentDrive)
			return true, nil
		case 0x0E: // 選磁碟機
			p.CurrentDrive = uint8(c.R16(cpu.DX))
			c.SetReg8(0, 26) // 回報有 26 台，夠用就好
			return true, nil
		case 0x47: // 取目前目錄：DS:SI 填不含磁碟機與前導反斜線的路徑
			sel, off := c.Seg[cpu.DS], c.R16(cpu.SI)
			for i, b := range append([]byte(p.CurrentDir), 0) {
				if err := p.Mod.Mem.WriteU8(sel, off+uint16(i), b); err != nil {
					return true, err
				}
			}
			c.SetR16(cpu.AX, 0x0100)
			c.SetFlag(cpu.FlagCF, false)
			return true, nil
		case 0x30: // 取 DOS 版本
			c.SetR16(cpu.AX, 0x1606) // 6.22：AL=6 主版本、AH=22 次版本
			c.SetR16(cpu.BX, 0)
			c.SetR16(cpu.CX, 0)
			return true, nil
		case 0x44: // IOCTL
			if uint8(c.R[cpu.AX]) != 0 {
				return false, nil
			}
			// AL=00：取裝置資訊。Borland 的啟動碼對 handle 0..4 各問一次，
			// 用來決定標準串流是文字還是二進位。一律回「字元裝置＋主控台」，
			// 這條路徑不影響畫面。
			c.SetR16(cpu.DX, 0x80D3)
			c.SetR16(cpu.AX, 0x80D3)
			c.SetFlag(cpu.FlagCF, false)
			return true, nil
		case 0x4C: // 結束行程
			c.Halt = true
			return true, &ExitError{Code: uint8(c.R16(cpu.AX))}
		}
	}
	return false, nil
}
