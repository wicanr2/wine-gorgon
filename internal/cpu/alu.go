package cpu

// 位寬用一個 bool 表示：true = 16 位元。指令表裡 w 位就是這個意思，
// 用同一個名字省下一層轉換。
func widthMask(w bool) uint32 {
	if w {
		return 0xFFFF
	}
	return 0xFF
}

func widthMSB(w bool) uint32 {
	if w {
		return 0x8000
	}
	return 0x80
}

func parity(v uint32) bool {
	v &= 0xFF
	v ^= v >> 4
	v ^= v >> 2
	v ^= v >> 1
	return v&1 == 0
}

// setSZP 設結果類旗標（SF／ZF／PF）。這三個只看結果，和運算種類無關。
func (c *CPU) setSZP(res uint32, w bool) {
	m := widthMask(w)
	c.SetFlag(FlagZF, res&m == 0)
	c.SetFlag(FlagSF, res&widthMSB(w) != 0)
	c.SetFlag(FlagPF, parity(res))
}

func (c *CPU) add(a, b uint32, w bool, carryIn uint32) uint32 {
	res := a + b + carryIn
	m := widthMask(w)
	c.SetFlag(FlagCF, res&^m != 0)
	c.SetFlag(FlagAF, (a^b^res)&0x10 != 0)
	c.SetFlag(FlagOF, (res^a)&(res^b)&widthMSB(w) != 0)
	c.setSZP(res, w)
	return res & m
}

func (c *CPU) sub(a, b uint32, w bool, borrowIn uint32) uint32 {
	res := a - b - borrowIn
	m := widthMask(w)
	c.SetFlag(FlagCF, res&^m != 0)
	c.SetFlag(FlagAF, (a^b^res)&0x10 != 0)
	c.SetFlag(FlagOF, (a^b)&(a^res)&widthMSB(w) != 0)
	c.setSZP(res, w)
	return res & m
}

// logic 是 AND／OR／XOR／TEST 共用的收尾：CF 與 OF 一律清掉。
func (c *CPU) logic(res uint32, w bool) uint32 {
	c.SetFlag(FlagCF, false)
	c.SetFlag(FlagOF, false)
	c.SetFlag(FlagAF, false)
	c.setSZP(res, w)
	return res & widthMask(w)
}

// inc／dec 不動 CF——這是它們和 add 1／sub 1 唯一的差別，
// 也是為什麼編譯器在需要保留進位的地方會挑它們。
func (c *CPU) inc(a uint32, w bool) uint32 {
	cf := c.Flag(FlagCF)
	res := c.add(a, 1, w, 0)
	c.SetFlag(FlagCF, cf)
	return res
}

func (c *CPU) dec(a uint32, w bool) uint32 {
	cf := c.Flag(FlagCF)
	res := c.sub(a, 1, w, 0)
	c.SetFlag(FlagCF, cf)
	return res
}

// aluOp 執行 ModRM 群組 0..7（ADD OR ADC SBB AND SUB XOR CMP）。
// 回傳結果與「要不要寫回」——CMP 是唯一不寫回的。
func (c *CPU) aluOp(op int, a, b uint32, w bool) (uint32, bool) {
	var cf uint32
	if c.Flag(FlagCF) {
		cf = 1
	}
	switch op {
	case 0:
		return c.add(a, b, w, 0), true
	case 1:
		return c.logic(a|b, w), true
	case 2:
		return c.add(a, b, w, cf), true
	case 3:
		return c.sub(a, b, w, cf), true
	case 4:
		return c.logic(a&b, w), true
	case 5:
		return c.sub(a, b, w, 0), true
	case 6:
		return c.logic(a^b, w), true
	default:
		return c.sub(a, b, w, 0), false
	}
}

// shiftOp 執行群組 2（ROL ROR RCL RCR SHL SHR SAL SAR）。
//
// 286 只取移位量的低 5 位（8086 不遮罩），這裡照 286 做——CIV.EXE 是
// 給 286 保護模式跑的。
func (c *CPU) shiftOp(op int, v uint32, count uint32, w bool) uint32 {
	m := widthMask(w)
	msb := widthMSB(w)
	bits := uint32(8)
	if w {
		bits = 16
	}
	count &= 0x1F
	if count == 0 {
		return v & m
	}
	v &= m
	var cf uint32
	if c.Flag(FlagCF) {
		cf = 1
	}

	switch op {
	case 0: // ROL
		n := count % bits
		res := (v<<n | v>>(bits-n)) & m
		if n == 0 {
			res = v
		}
		c.SetFlag(FlagCF, res&1 != 0)
		c.SetFlag(FlagOF, (res&msb != 0) != (res&1 != 0))
		return res
	case 1: // ROR
		n := count % bits
		res := (v>>n | v<<(bits-n)) & m
		if n == 0 {
			res = v
		}
		c.SetFlag(FlagCF, res&msb != 0)
		c.SetFlag(FlagOF, (res&msb != 0) != (res&(msb>>1) != 0))
		return res
	case 2: // RCL
		n := count % (bits + 1)
		for i := uint32(0); i < n; i++ {
			nc := (v & msb) >> (bits - 1)
			v = (v<<1 | cf) & m
			cf = nc
		}
		c.SetFlag(FlagCF, cf != 0)
		c.SetFlag(FlagOF, (v&msb != 0) != (cf != 0))
		return v
	case 3: // RCR
		n := count % (bits + 1)
		for i := uint32(0); i < n; i++ {
			nc := v & 1
			v = v>>1 | cf<<(bits-1)
			cf = nc
		}
		c.SetFlag(FlagCF, cf != 0)
		c.SetFlag(FlagOF, (v&msb != 0) != (v&(msb>>1) != 0))
		return v & m
	case 4, 6: // SHL／SAL
		res := v << count
		c.SetFlag(FlagCF, res&(m+1) != 0)
		if count > bits {
			c.SetFlag(FlagCF, false)
		}
		res &= m
		c.SetFlag(FlagOF, (res&msb != 0) != c.Flag(FlagCF))
		c.setSZP(res, w)
		return res
	case 5: // SHR
		c.SetFlag(FlagCF, count <= bits && v>>(count-1)&1 != 0)
		res := v >> count
		if count > bits {
			res = 0
		}
		c.SetFlag(FlagOF, v&msb != 0)
		c.setSZP(res, w)
		return res & m
	default: // SAR
		sv := int32(int16(v))
		if !w {
			sv = int32(int8(v))
		}
		n := count
		if n > bits {
			n = bits
		}
		c.SetFlag(FlagCF, sv>>(n-1)&1 != 0)
		res := uint32(sv>>n) & m
		c.SetFlag(FlagOF, false)
		c.setSZP(res, w)
		return res
	}
}
