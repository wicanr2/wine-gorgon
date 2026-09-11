package cpu

// 字串指令。REP 前綴在這裡展開成迴圈，一次 Step 做完整串——
// 這對「重現畫面」是安全的（沒有中斷要插進來），而且省掉每個位元組
// 一次 dispatch。
//
// 方向由 DF 決定；`si`／`di` 的段別不同：來源可被前綴覆寫，
// **目的地永遠是 ES**，覆寫不了。這是 x86 少數不對稱的地方。

func (c *CPU) strDelta(sz Size) uint32 {
	d := uint32(sz)
	if c.Flag(FlagDF) {
		return -d
	}
	return d
}

// idx 讀索引暫存器（SI／DI）。位寬跟 `67` 前綴走，不是跟運算元走：
// `67 AC` 是「用 ESI 取一個 byte」。只取低 16 位的話，掃過 64 KiB 的
// 迴圈會在第 65,536 個位元組繞回開頭，而且不會報錯。
func (c *CPU) idx(i int) uint32 {
	if c.addrSize == S32 {
		return c.R[i]
	}
	return uint32(c.R16(i))
}

// setIdx 寫回索引暫存器，位寬同上。
func (c *CPU) setIdx(i int, v uint32) {
	if c.addrSize == S32 {
		c.R[i] = v
		return
	}
	c.SetR16(i, uint16(v))
}

// counter 讀 REP 的計數器（CX／ECX），位寬同樣跟 `67` 走。
func (c *CPU) counter() uint32 {
	if c.addrSize == S32 {
		return c.R[CX]
	}
	return uint32(c.R16(CX))
}

func (c *CPU) setCounter(v uint32) {
	if c.addrSize == S32 {
		c.R[CX] = v
		return
	}
	c.SetR16(CX, uint16(v))
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
		if rep != 0 && c.counter() == 0 {
			return nil
		}

		switch op {
		case 0xA4: // MOVS
			v, err := c.busRead(src, c.idx(SI), sz)
			if err != nil {
				return err
			}
			if err := c.busWrite(c.Seg[ES], c.idx(DI), sz, v); err != nil {
				return err
			}
			c.setIdx(SI, c.idx(SI)+d)
			c.setIdx(DI, c.idx(DI)+d)
		case 0xA6: // CMPS：比的是 [SI] - [DI]
			a, err := c.busRead(src, c.idx(SI), sz)
			if err != nil {
				return err
			}
			b, err := c.busRead(c.Seg[ES], c.idx(DI), sz)
			if err != nil {
				return err
			}
			c.sub(a, b, sz, 0)
			c.setIdx(SI, c.idx(SI)+d)
			c.setIdx(DI, c.idx(DI)+d)
		case 0xAA: // STOS
			if err := c.busWrite(c.Seg[ES], c.idx(DI), sz, c.acc(sz)); err != nil {
				return err
			}
			c.setIdx(DI, c.idx(DI)+d)
		case 0xAC: // LODS
			v, err := c.busRead(src, c.idx(SI), sz)
			if err != nil {
				return err
			}
			c.setAcc(v, sz)
			c.setIdx(SI, c.idx(SI)+d)
		case 0xAE: // SCAS：比的是 AL/AX/EAX - [DI]
			b, err := c.busRead(c.Seg[ES], c.idx(DI), sz)
			if err != nil {
				return err
			}
			c.sub(c.acc(sz), b, sz, 0)
			c.setIdx(DI, c.idx(DI)+d)
		}

		if rep == 0 {
			return nil
		}
		c.setCounter(c.counter() - 1)

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
