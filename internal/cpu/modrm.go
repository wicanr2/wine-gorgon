package cpu

// operand 是一條指令解出來的目的地／來源。
// isReg 為真時看 reg（8 位元時是 ModRM 的 8 位元暫存器編碼）；
// 否則看 sel:off。
type operand struct {
	isReg bool
	reg   int
	sel   uint16
	off   uint16
}

// modrm 是解完的 ModRM 位元組。
type modrm struct {
	mod int
	reg int // 中間三位（可能是暫存器，也可能是 opcode 擴充）
	rm  operand
}

// decodeModRM 讀一個 ModRM 位元組與它後面的位移。
//
// 16 位元定址的預設段：**只要基底用到 BP 就是 SS**，其餘是 DS
// （`[BP+SI]`／`[BP+DI]`／`[BP+disp]` 三種）。這條規則是 stack frame
// 能運作的前提，寫錯的話局部變數會去讀資料段，而且不會當掉，只會拿到
// 看起來合理的垃圾。
func (c *CPU) decodeModRM() (modrm, error) {
	b, err := c.fetch8()
	if err != nil {
		return modrm{}, err
	}
	m := modrm{mod: int(b >> 6), reg: int(b>>3) & 7}
	rm := int(b) & 7

	if m.mod == 3 {
		m.rm = operand{isReg: true, reg: rm}
		return m, nil
	}

	var base uint16
	defSeg := DS
	switch rm {
	case 0:
		base = c.R16(BX) + c.R16(SI)
	case 1:
		base = c.R16(BX) + c.R16(DI)
	case 2:
		base = c.R16(BP) + c.R16(SI)
		defSeg = SS
	case 3:
		base = c.R16(BP) + c.R16(DI)
		defSeg = SS
	case 4:
		base = c.R16(SI)
	case 5:
		base = c.R16(DI)
	case 6:
		if m.mod == 0 {
			// mod=00 rm=110 是「純 disp16」，不是 [BP]。
			d, err := c.fetch16()
			if err != nil {
				return modrm{}, err
			}
			m.rm = operand{sel: c.dataSeg(DS), off: d}
			return m, nil
		}
		base = c.R16(BP)
		defSeg = SS
	case 7:
		base = c.R16(BX)
	}

	switch m.mod {
	case 1:
		d, err := c.fetch8()
		if err != nil {
			return modrm{}, err
		}
		base += uint16(int16(int8(d)))
	case 2:
		d, err := c.fetch16()
		if err != nil {
			return modrm{}, err
		}
		base += d
	}
	m.rm = operand{sel: c.dataSeg(defSeg), off: base}
	return m, nil
}

// --- 運算元讀寫 ---

func (c *CPU) readOp(o operand, sz Size) (uint32, error) {
	if o.isReg {
		return c.reg(o.reg, sz), nil
	}
	return c.busRead(o.sel, o.off, sz)
}

func (c *CPU) writeOp(o operand, v uint32, sz Size) error {
	if o.isReg {
		c.setReg(o.reg, v, sz)
		return nil
	}
	return c.busWrite(o.sel, o.off, sz, v)
}
