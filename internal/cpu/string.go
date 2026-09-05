package cpu

// 字串指令。REP 前綴在這裡展開成迴圈，一次 Step 做完整串——
// 這對「重現畫面」是安全的（沒有中斷要插進來），而且省掉每個位元組
// 一次 dispatch。
//
// 方向由 DF 決定；`si`／`di` 的段別不同：來源可被前綴覆寫，
// **目的地永遠是 ES**，覆寫不了。這是 x86 少數不對稱的地方。

func (c *CPU) strDelta(w bool) uint16 {
	d := uint16(1)
	if w {
		d = 2
	}
	if c.Flag(FlagDF) {
		return -d
	}
	return d
}

func (c *CPU) readSrc(w bool) (uint32, error) {
	seg := c.dataSeg(DS)
	if w {
		v, err := c.Bus.ReadU16(seg, c.R[SI])
		return uint32(v), err
	}
	v, err := c.Bus.ReadU8(seg, c.R[SI])
	return uint32(v), err
}

func (c *CPU) readDst(w bool) (uint32, error) {
	if w {
		v, err := c.Bus.ReadU16(c.Seg[ES], c.R[DI])
		return uint32(v), err
	}
	v, err := c.Bus.ReadU8(c.Seg[ES], c.R[DI])
	return uint32(v), err
}

func (c *CPU) writeDst(v uint32, w bool) error {
	if w {
		return c.Bus.WriteU16(c.Seg[ES], c.R[DI], uint16(v))
	}
	return c.Bus.WriteU8(c.Seg[ES], c.R[DI], uint8(v))
}

func (c *CPU) acc(w bool) uint32 {
	if w {
		return uint32(c.R[AX])
	}
	return uint32(c.Reg8(0))
}

func (c *CPU) setAcc(v uint32, w bool) {
	if w {
		c.R[AX] = uint16(v)
		return
	}
	c.SetReg8(0, uint8(v))
}

// stringOp 跑一條字串指令（含 REP）。op 用主 opcode 的低位表示：
// 0xA4 movs、0xA6 cmps、0xAA stos、0xAC lods、0xAE scas。
func (c *CPU) stringOp(op uint8, w bool) error {
	rep := c.repPrefix
	d := c.strDelta(w)

	for {
		if rep != 0 {
			if c.R[CX] == 0 {
				return nil
			}
		}

		switch op {
		case 0xA4: // MOVS
			v, err := c.readSrc(w)
			if err != nil {
				return err
			}
			if err := c.writeDst(v, w); err != nil {
				return err
			}
			c.R[SI] += d
			c.R[DI] += d
		case 0xA6: // CMPS：比的是 [SI] - [DI]
			a, err := c.readSrc(w)
			if err != nil {
				return err
			}
			b, err := c.readDst(w)
			if err != nil {
				return err
			}
			c.sub(a, b, w, 0)
			c.R[SI] += d
			c.R[DI] += d
		case 0xAA: // STOS
			if err := c.writeDst(c.acc(w), w); err != nil {
				return err
			}
			c.R[DI] += d
		case 0xAC: // LODS
			v, err := c.readSrc(w)
			if err != nil {
				return err
			}
			c.setAcc(v, w)
			c.R[SI] += d
		case 0xAE: // SCAS：比的是 AL/AX - [DI]
			b, err := c.readDst(w)
			if err != nil {
				return err
			}
			c.sub(c.acc(w), b, w, 0)
			c.R[DI] += d
		}

		if rep == 0 {
			return nil
		}
		c.R[CX]--

		// REP 對 CMPS／SCAS 才看 ZF；對 MOVS／STOS／LODS，
		// F2 和 F3 是同一件事（只看 CX）。
		if op == 0xA6 || op == 0xAE {
			if rep == 0xF3 && !c.Flag(FlagZF) {
				return nil
			}
			if rep == 0xF2 && c.Flag(FlagZF) {
				return nil
			}
		}
	}
}
