package cpu

// 字串指令。REP 前綴在這裡展開成迴圈，一次 Step 做完整串——
// 這對「重現畫面」是安全的（沒有中斷要插進來），而且省掉每個位元組
// 一次 dispatch。
//
// 方向由 DF 決定；`si`／`di` 的段別不同：來源可被前綴覆寫，
// **目的地永遠是 ES**，覆寫不了。這是 x86 少數不對稱的地方。

func (c *CPU) strDelta(sz Size) uint16 {
	d := uint16(sz)
	if c.Flag(FlagDF) {
		return -d
	}
	return d
}

func (c *CPU) acc(sz Size) uint32 { return c.reg(AX, sz) }

func (c *CPU) setAcc(v uint32, sz Size) { c.setReg(AX, v, sz) }

// stringOp 跑一條字串指令（含 REP）。op 用主 opcode 的偶數形表示：
// 0xA4 movs、0xA6 cmps、0xAA stos、0xAC lods、0xAE scas。
func (c *CPU) stringOp(op uint8, sz Size) error {
	rep := c.repPrefix
	d := c.strDelta(sz)
	src := c.dataSeg(DS)

	for {
		if rep != 0 && c.R16(CX) == 0 {
			return nil
		}

		switch op {
		case 0xA4: // MOVS
			v, err := c.busRead(src, c.R16(SI), sz)
			if err != nil {
				return err
			}
			if err := c.busWrite(c.Seg[ES], c.R16(DI), sz, v); err != nil {
				return err
			}
			c.SetR16(SI, c.R16(SI)+d)
			c.SetR16(DI, c.R16(DI)+d)
		case 0xA6: // CMPS：比的是 [SI] - [DI]
			a, err := c.busRead(src, c.R16(SI), sz)
			if err != nil {
				return err
			}
			b, err := c.busRead(c.Seg[ES], c.R16(DI), sz)
			if err != nil {
				return err
			}
			c.sub(a, b, sz, 0)
			c.SetR16(SI, c.R16(SI)+d)
			c.SetR16(DI, c.R16(DI)+d)
		case 0xAA: // STOS
			if err := c.busWrite(c.Seg[ES], c.R16(DI), sz, c.acc(sz)); err != nil {
				return err
			}
			c.SetR16(DI, c.R16(DI)+d)
		case 0xAC: // LODS
			v, err := c.busRead(src, c.R16(SI), sz)
			if err != nil {
				return err
			}
			c.setAcc(v, sz)
			c.SetR16(SI, c.R16(SI)+d)
		case 0xAE: // SCAS：比的是 AL/AX/EAX - [DI]
			b, err := c.busRead(c.Seg[ES], c.R16(DI), sz)
			if err != nil {
				return err
			}
			c.sub(c.acc(sz), b, sz, 0)
			c.SetR16(DI, c.R16(DI)+d)
		}

		if rep == 0 {
			return nil
		}
		c.SetR16(CX, c.R16(CX)-1)

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
