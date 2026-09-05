package cpu

func widthMask(sz Size) uint32 {
	switch sz {
	case S8:
		return 0xFF
	case S32:
		return 0xFFFFFFFF
	default:
		return 0xFFFF
	}
}

func widthMSB(sz Size) uint32 {
	switch sz {
	case S8:
		return 0x80
	case S32:
		return 0x80000000
	default:
		return 0x8000
	}
}

func widthBits(sz Size) uint32 { return uint32(sz) * 8 }

// signExtend 把一個 sz 寬的值當成有號數展開成 int32。
func signExtend(v uint32, sz Size) int32 {
	switch sz {
	case S8:
		return int32(int8(v))
	case S32:
		return int32(v)
	default:
		return int32(int16(v))
	}
}

func parity(v uint32) bool {
	v &= 0xFF
	v ^= v >> 4
	v ^= v >> 2
	v ^= v >> 1
	return v&1 == 0
}

// setSZP 設結果類旗標（SF／ZF／PF）。這三個只看結果，和運算種類無關。
func (c *CPU) setSZP(res uint32, sz Size) {
	m := widthMask(sz)
	c.SetFlag(FlagZF, res&m == 0)
	c.SetFlag(FlagSF, res&widthMSB(sz) != 0)
	c.SetFlag(FlagPF, parity(res))
}

// add 與 sub 用 64 位元中間值，這樣 32 位元運算的進位判斷不必特別處理。
func (c *CPU) add(a, b uint32, sz Size, carryIn uint32) uint32 {
	m := widthMask(sz)
	full := uint64(a&m) + uint64(b&m) + uint64(carryIn)
	res := uint32(full) & m
	c.SetFlag(FlagCF, full>>widthBits(sz) != 0)
	c.SetFlag(FlagAF, (a^b^res)&0x10 != 0)
	c.SetFlag(FlagOF, (res^a)&(res^b)&widthMSB(sz) != 0)
	c.setSZP(res, sz)
	return res
}

func (c *CPU) sub(a, b uint32, sz Size, borrowIn uint32) uint32 {
	m := widthMask(sz)
	full := uint64(a&m) - uint64(b&m) - uint64(borrowIn)
	res := uint32(full) & m
	c.SetFlag(FlagCF, full>>widthBits(sz)&1 != 0)
	c.SetFlag(FlagAF, (a^b^res)&0x10 != 0)
	c.SetFlag(FlagOF, (a^b)&(a^res)&widthMSB(sz) != 0)
	c.setSZP(res, sz)
	return res
}

// logic 是 AND／OR／XOR／TEST 共用的收尾：CF 與 OF 一律清掉。
func (c *CPU) logic(res uint32, sz Size) uint32 {
	c.SetFlag(FlagCF, false)
	c.SetFlag(FlagOF, false)
	c.SetFlag(FlagAF, false)
	c.setSZP(res, sz)
	return res & widthMask(sz)
}

// inc／dec 不動 CF——這是它們和 add 1／sub 1 唯一的差別，
// 也是為什麼編譯器在需要保留進位的地方會挑它們。
func (c *CPU) inc(a uint32, sz Size) uint32 {
	cf := c.Flag(FlagCF)
	res := c.add(a, 1, sz, 0)
	c.SetFlag(FlagCF, cf)
	return res
}

func (c *CPU) dec(a uint32, sz Size) uint32 {
	cf := c.Flag(FlagCF)
	res := c.sub(a, 1, sz, 0)
	c.SetFlag(FlagCF, cf)
	return res
}

// aluOp 執行 ModRM 群組 0..7（ADD OR ADC SBB AND SUB XOR CMP）。
// 回傳結果與「要不要寫回」——CMP 是唯一不寫回的。
func (c *CPU) aluOp(op int, a, b uint32, sz Size) (uint32, bool) {
	var cf uint32
	if c.Flag(FlagCF) {
		cf = 1
	}
	switch op {
	case 0:
		return c.add(a, b, sz, 0), true
	case 1:
		return c.logic(a|b, sz), true
	case 2:
		return c.add(a, b, sz, cf), true
	case 3:
		return c.sub(a, b, sz, cf), true
	case 4:
		return c.logic(a&b, sz), true
	case 5:
		return c.sub(a, b, sz, 0), true
	case 6:
		return c.logic(a^b, sz), true
	default:
		return c.sub(a, b, sz, 0), false
	}
}

// shiftOp 執行群組 2（ROL ROR RCL RCR SHL SHR SAL SAR）。
//
// 286 以後只取移位量的低 5 位（8086 不遮罩），這裡照 286 做。
func (c *CPU) shiftOp(op int, v uint32, count uint32, sz Size) uint32 {
	m := widthMask(sz)
	msb := widthMSB(sz)
	bits := widthBits(sz)
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
		res := v
		if n != 0 {
			res = (v<<n | v>>(bits-n)) & m
		}
		c.SetFlag(FlagCF, res&1 != 0)
		c.SetFlag(FlagOF, (res&msb != 0) != (res&1 != 0))
		return res
	case 1: // ROR
		n := count % bits
		res := v
		if n != 0 {
			res = (v>>n | v<<(bits-n)) & m
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
		var res uint32
		if count < bits {
			res = (v << count) & m
			c.SetFlag(FlagCF, v>>(bits-count)&1 != 0)
		} else if count == bits {
			c.SetFlag(FlagCF, v&1 != 0)
		} else {
			c.SetFlag(FlagCF, false)
		}
		c.SetFlag(FlagOF, (res&msb != 0) != c.Flag(FlagCF))
		c.setSZP(res, sz)
		return res
	case 5: // SHR
		var res uint32
		if count <= bits {
			c.SetFlag(FlagCF, v>>(count-1)&1 != 0)
		} else {
			c.SetFlag(FlagCF, false)
		}
		if count < bits {
			res = v >> count
		}
		c.SetFlag(FlagOF, v&msb != 0)
		c.setSZP(res, sz)
		return res & m
	default: // SAR
		sv := signExtend(v, sz)
		// CF 是「最後被移出去的那一位」＝原值的第 count-1 位；
		// 移過頭時整個結果都是符號位，CF 也就是符號位。
		n := count
		if n > bits {
			n = bits
		}
		c.SetFlag(FlagCF, sv>>(n-1)&1 != 0)
		shift := count
		if shift >= bits {
			shift = bits - 1
		}
		res := uint32(sv>>shift) & m
		c.SetFlag(FlagOF, false)
		c.setSZP(res, sz)
		return res
	}
}
