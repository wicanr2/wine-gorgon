package cpu

// Step 執行一條指令（含前綴）。
func (c *CPU) Step() error {
	c.segOverride = -1
	c.repPrefix = 0
	c.opSize = S16
	startIP := c.IP

	for {
		op, err := c.fetch8()
		if err != nil {
			return c.wrap(startIP, err, "取指失敗")
		}
		switch op {
		case 0x26:
			c.segOverride = ES
			continue
		case 0x2E:
			c.segOverride = CS
			continue
		case 0x36:
			c.segOverride = SS
			continue
		case 0x3E:
			c.segOverride = DS
			continue
		case 0x64, 0x65:
			return c.errf(startIP, "FS／GS 段覆寫前綴 %02X（386 以上，未實作）", op)
		case 0x66:
			c.opSize = S32
			continue
		case 0x67:
			return c.errf(startIP, "位址大小前綴 67（32 位元定址，未實作）")
		case 0xF0: // LOCK：單執行緒下沒有語意
			continue
		case 0xF2, 0xF3:
			c.repPrefix = op
			continue
		}

		c.Steps++
		if err := c.exec(op, startIP); err != nil {
			return err
		}
		return nil
	}
}

// Run 連跑到 Halt、錯誤或步數上限。maxSteps <= 0 表示不設上限。
func (c *CPU) Run(maxSteps uint64) error {
	for n := uint64(0); maxSteps <= 0 || n < maxSteps; n++ {
		if c.Halt {
			return nil
		}
		if err := c.Step(); err != nil {
			return err
		}
	}
	return nil
}

// RetFar 模擬 `retf popBytes`：API 處理器做完事之後呼叫它回到呼叫端。
// Win16 是 pascal 呼叫慣例，參數由**被呼叫方**清掉，所以 popBytes 是
// 那支 API 的參數位元組數。
func (c *CPU) RetFar(popBytes uint16) error {
	ip, err := c.pop16()
	if err != nil {
		return err
	}
	cs, err := c.pop16()
	if err != nil {
		return err
	}
	c.IP, c.Seg[CS] = ip, cs
	c.SetR16(SP, c.R16(SP)+popBytes)
	return nil
}

func (c *CPU) farTransfer(sel, off uint16, isCall bool) error {
	if isCall {
		if err := c.push16(c.Seg[CS]); err != nil {
			return err
		}
		if err := c.push16(c.IP); err != nil {
			return err
		}
	}
	c.Seg[CS], c.IP = sel, off
	if c.OnFarCall != nil {
		if _, err := c.OnFarCall(c, sel, off); err != nil {
			return err
		}
	}
	return nil
}

// cond 算條件碼 0..15（Jcc 那張表）。偶數是「條件成立」，
// 奇數是同一個條件取反——所以先算 n>>1 再看最低位。
func (c *CPU) cond(n int) bool {
	var v bool
	switch n >> 1 {
	case 0:
		v = c.Flag(FlagOF)
	case 1:
		v = c.Flag(FlagCF)
	case 2:
		v = c.Flag(FlagZF)
	case 3:
		v = c.Flag(FlagCF) || c.Flag(FlagZF)
	case 4:
		v = c.Flag(FlagSF)
	case 5:
		v = c.Flag(FlagPF)
	case 6:
		v = c.Flag(FlagSF) != c.Flag(FlagOF)
	default:
		v = c.Flag(FlagZF) || (c.Flag(FlagSF) != c.Flag(FlagOF))
	}
	if n&1 == 1 {
		return !v
	}
	return v
}

// opSizeFor 依 opcode 的 w 位決定位寬：w=0 一律是 8 位元，
// w=1 是這條指令的運算元大小（預設 16，有 66 前綴就是 32）。
func (c *CPU) opSizeFor(op uint8) Size {
	if op&1 == 0 {
		return S8
	}
	return c.opSize
}

func (c *CPU) exec(op uint8, ip uint16) error {
	// --- 0x00..0x3F：八個 ALU 運算 ＋ 段暫存器推入彈出 ---
	if op < 0x40 {
		grp := int(op >> 3)
		switch op & 7 {
		case 0, 1: // op r/m, r
			sz := c.opSizeFor(op)
			m, err := c.decodeModRM()
			if err != nil {
				return c.wrap(ip, err, "ModRM")
			}
			a, err := c.readOp(m.rm, sz)
			if err != nil {
				return c.wrap(ip, err, "讀運算元")
			}
			res, store := c.aluOp(grp, a, c.reg(m.reg, sz), sz)
			if store {
				return c.wrap(ip, c.writeOp(m.rm, res, sz), "寫回")
			}
			return nil
		case 2, 3: // op r, r/m
			sz := c.opSizeFor(op)
			m, err := c.decodeModRM()
			if err != nil {
				return c.wrap(ip, err, "ModRM")
			}
			b, err := c.readOp(m.rm, sz)
			if err != nil {
				return c.wrap(ip, err, "讀運算元")
			}
			res, store := c.aluOp(grp, c.reg(m.reg, sz), b, sz)
			if store {
				c.setReg(m.reg, res, sz)
			}
			return nil
		case 4, 5: // op acc, imm
			sz := c.opSizeFor(op)
			imm, err := c.fetchImm(sz)
			if err != nil {
				return c.wrap(ip, err, "立即數")
			}
			res, store := c.aluOp(grp, c.acc(sz), imm, sz)
			if store {
				c.setAcc(res, sz)
			}
			return nil
		case 6: // push sreg（0x0E 是 push cs）
			if grp > 3 {
				return c.errf(ip, "未實作的 opcode %02X", op)
			}
			return c.wrap(ip, c.push16(c.Seg[grp]), "push 段暫存器")
		default: // pop sreg；0x0F 在 286 以後是雙位元組跳脫，不是 pop cs
			if op == 0x0F {
				return c.exec0F(ip)
			}
			if grp > 3 {
				return c.errf(ip, "未實作的 opcode %02X", op)
			}
			v, err := c.pop16()
			if err != nil {
				return c.wrap(ip, err, "pop 段暫存器")
			}
			c.Seg[grp] = v
			return nil
		}
	}

	switch {
	case op >= 0x40 && op <= 0x47: // INC r
		i := int(op - 0x40)
		c.setReg(i, c.inc(c.reg(i, c.opSize), c.opSize), c.opSize)
		return nil
	case op >= 0x48 && op <= 0x4F: // DEC r
		i := int(op - 0x48)
		c.setReg(i, c.dec(c.reg(i, c.opSize), c.opSize), c.opSize)
		return nil
	case op >= 0x50 && op <= 0x57: // PUSH r
		return c.wrap(ip, c.pushSize(c.reg(int(op-0x50), c.opSize), c.opSize), "push")
	case op >= 0x58 && op <= 0x5F: // POP r
		v, err := c.popSize(c.opSize)
		if err != nil {
			return c.wrap(ip, err, "pop")
		}
		c.setReg(int(op-0x58), v, c.opSize)
		return nil
	case op >= 0x70 && op <= 0x7F: // Jcc rel8
		d, err := c.fetch8()
		if err != nil {
			return c.wrap(ip, err, "取 rel8")
		}
		if c.cond(int(op - 0x70)) {
			c.IP += uint16(int16(int8(d)))
		}
		return nil
	case op >= 0x91 && op <= 0x97: // XCHG acc, r
		r := int(op - 0x90)
		a, b := c.reg(AX, c.opSize), c.reg(r, c.opSize)
		c.setReg(AX, b, c.opSize)
		c.setReg(r, a, c.opSize)
		return nil
	case op >= 0xB0 && op <= 0xB7: // MOV r8, imm8
		v, err := c.fetch8()
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		c.SetReg8(int(op-0xB0), v)
		return nil
	case op >= 0xB8 && op <= 0xBF: // MOV r, imm
		v, err := c.fetchImm(c.opSize)
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		c.setReg(int(op-0xB8), v, c.opSize)
		return nil
	}

	switch op {
	case 0x60: // PUSHA／PUSHAD
		sp := c.reg(SP, c.opSize)
		for i := 0; i < 8; i++ {
			v := c.reg(i, c.opSize)
			if i == SP {
				v = sp
			}
			if err := c.pushSize(v, c.opSize); err != nil {
				return c.wrap(ip, err, "pusha")
			}
		}
		return nil
	case 0x61: // POPA（SP 那一格丟掉）
		for i := 7; i >= 0; i-- {
			v, err := c.popSize(c.opSize)
			if err != nil {
				return c.wrap(ip, err, "popa")
			}
			if i != SP {
				c.setReg(i, v, c.opSize)
			}
		}
		return nil
	case 0x68: // PUSH imm
		v, err := c.fetchImm(c.opSize)
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		return c.wrap(ip, c.pushSize(v, c.opSize), "push")
	case 0x6A: // PUSH imm8（符號延伸到運算元寬度）
		v, err := c.fetch8()
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		return c.wrap(ip, c.pushSize(uint32(int32(int8(v))), c.opSize), "push")
	case 0x69, 0x6B: // IMUL r, r/m, imm
		sz := c.opSize
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		src, err := c.readOp(m.rm, sz)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		var imm int64
		if op == 0x69 {
			v, err := c.fetchImm(sz)
			if err != nil {
				return c.wrap(ip, err, "立即數")
			}
			imm = int64(signExtend(v, sz))
		} else {
			v, err := c.fetch8()
			if err != nil {
				return c.wrap(ip, err, "立即數")
			}
			imm = int64(int8(v))
		}
		full := int64(signExtend(src, sz)) * imm
		c.setReg(m.reg, uint32(full), sz)
		over := full != int64(signExtend(uint32(full), sz))
		c.SetFlag(FlagCF, over)
		c.SetFlag(FlagOF, over)
		return nil
	case 0x80, 0x81, 0x82, 0x83: // 群組 1：op r/m, imm
		sz := c.opSizeFor(op)
		if op == 0x82 {
			sz = S8
		}
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		var imm uint32
		if op == 0x81 {
			v, err := c.fetchImm(sz)
			if err != nil {
				return c.wrap(ip, err, "立即數")
			}
			imm = v
		} else {
			v, err := c.fetch8()
			if err != nil {
				return c.wrap(ip, err, "立即數")
			}
			if op == 0x83 {
				imm = uint32(int32(int8(v))) & widthMask(sz) // 0x83 是符號延伸
			} else {
				imm = uint32(v)
			}
		}
		a, err := c.readOp(m.rm, sz)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		res, store := c.aluOp(m.reg, a, imm, sz)
		if store {
			return c.wrap(ip, c.writeOp(m.rm, res, sz), "寫回")
		}
		return nil
	case 0x84, 0x85: // TEST r/m, r
		sz := c.opSizeFor(op)
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		a, err := c.readOp(m.rm, sz)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		c.logic(a&c.reg(m.reg, sz), sz)
		return nil
	case 0x86, 0x87: // XCHG r/m, r
		sz := c.opSizeFor(op)
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		a, err := c.readOp(m.rm, sz)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		b := c.reg(m.reg, sz)
		if err := c.writeOp(m.rm, b, sz); err != nil {
			return c.wrap(ip, err, "寫回")
		}
		c.setReg(m.reg, a, sz)
		return nil
	case 0x88, 0x89: // MOV r/m, r
		sz := c.opSizeFor(op)
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		return c.wrap(ip, c.writeOp(m.rm, c.reg(m.reg, sz), sz), "寫回")
	case 0x8A, 0x8B: // MOV r, r/m
		sz := c.opSizeFor(op)
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		v, err := c.readOp(m.rm, sz)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		c.setReg(m.reg, v, sz)
		return nil
	case 0x8C: // MOV r/m16, sreg
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		return c.wrap(ip, c.writeOp(m.rm, uint32(c.Seg[m.reg&3]), S16), "寫回")
	case 0x8D: // LEA：只算位址，不碰記憶體
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		if m.rm.isReg {
			return c.errf(ip, "LEA 的運算元是暫存器（無效編碼）")
		}
		c.setReg(m.reg, uint32(m.rm.off), c.opSize)
		return nil
	case 0x8E: // MOV sreg, r/m16
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		v, err := c.readOp(m.rm, S16)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		c.Seg[m.reg&3] = uint16(v)
		return nil
	case 0x8F: // POP r/m
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		v, err := c.popSize(c.opSize)
		if err != nil {
			return c.wrap(ip, err, "pop")
		}
		return c.wrap(ip, c.writeOp(m.rm, v, c.opSize), "寫回")
	case 0x90: // NOP
		return nil
	case 0x98: // CBW／CWDE
		if c.opSize == S32 {
			c.R[AX] = uint32(int32(int16(c.R16(AX))))
		} else {
			c.SetR16(AX, uint16(int16(int8(c.Reg8(0)))))
		}
		return nil
	case 0x99: // CWD／CDQ
		if c.opSize == S32 {
			if c.R[AX]&0x80000000 != 0 {
				c.R[DX] = 0xFFFFFFFF
			} else {
				c.R[DX] = 0
			}
			return nil
		}
		if c.R16(AX)&0x8000 != 0 {
			c.SetR16(DX, 0xFFFF)
		} else {
			c.SetR16(DX, 0)
		}
		return nil
	case 0x9A: // CALL far imm
		off, err := c.fetch16()
		if err != nil {
			return c.wrap(ip, err, "far call 位移")
		}
		sel, err := c.fetch16()
		if err != nil {
			return c.wrap(ip, err, "far call selector")
		}
		return c.wrap(ip, c.farTransfer(sel, off, true), "far call")
	case 0x9B: // WAIT
		return nil
	case 0x9C: // PUSHF
		return c.wrap(ip, c.push16(c.Flags), "pushf")
	case 0x9D: // POPF
		v, err := c.pop16()
		if err != nil {
			return c.wrap(ip, err, "popf")
		}
		c.Flags = v&0x0FD5 | 0x0002
		return nil
	case 0x9E: // SAHF
		c.Flags = c.Flags&0xFF00 | uint16(c.Reg8(4))&0xD5 | 0x0002
		return nil
	case 0x9F: // LAHF
		c.SetReg8(4, uint8(c.Flags))
		return nil
	case 0xA0, 0xA1, 0xA2, 0xA3: // MOV acc, [imm16] 與反向
		sz := c.opSizeFor(op)
		d, err := c.fetch16()
		if err != nil {
			return c.wrap(ip, err, "位移")
		}
		o := operand{sel: c.dataSeg(DS), off: d}
		if op < 0xA2 {
			v, err := c.readOp(o, sz)
			if err != nil {
				return c.wrap(ip, err, "讀運算元")
			}
			c.setAcc(v, sz)
			return nil
		}
		return c.wrap(ip, c.writeOp(o, c.acc(sz), sz), "寫回")
	case 0xA4, 0xA5, 0xA6, 0xA7, 0xAA, 0xAB, 0xAC, 0xAD, 0xAE, 0xAF:
		return c.wrap(ip, c.stringOp(op&^1, c.opSizeFor(op)), "字串指令")
	case 0xA8, 0xA9: // TEST acc, imm
		sz := c.opSizeFor(op)
		imm, err := c.fetchImm(sz)
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		c.logic(c.acc(sz)&imm, sz)
		return nil
	case 0xC0, 0xC1, 0xD0, 0xD1, 0xD2, 0xD3: // 群組 2：位移／旋轉
		sz := c.opSizeFor(op)
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		var count uint32
		switch op {
		case 0xC0, 0xC1:
			v, err := c.fetch8()
			if err != nil {
				return c.wrap(ip, err, "移位量")
			}
			count = uint32(v)
		case 0xD0, 0xD1:
			count = 1
		default:
			count = uint32(c.Reg8(1)) // CL
		}
		a, err := c.readOp(m.rm, sz)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		return c.wrap(ip, c.writeOp(m.rm, c.shiftOp(m.reg, a, count, sz), sz), "寫回")
	case 0xC2, 0xC3: // RET near
		var pop uint16
		if op == 0xC2 {
			v, err := c.fetch16()
			if err != nil {
				return c.wrap(ip, err, "ret 立即數")
			}
			pop = v
		}
		v, err := c.pop16()
		if err != nil {
			return c.wrap(ip, err, "ret")
		}
		c.IP = v
		c.SetR16(SP, c.R16(SP)+pop)
		return nil
	case 0xC4, 0xC5: // LES／LDS
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		if m.rm.isReg {
			return c.errf(ip, "LES/LDS 的運算元是暫存器（無效編碼）")
		}
		off, err := c.Bus.ReadU16(m.rm.sel, m.rm.off)
		if err != nil {
			return c.wrap(ip, err, "讀 far 指標位移")
		}
		sel, err := c.Bus.ReadU16(m.rm.sel, m.rm.off+2)
		if err != nil {
			return c.wrap(ip, err, "讀 far 指標 selector")
		}
		c.SetR16(m.reg, off)
		if op == 0xC4 {
			c.Seg[ES] = sel
		} else {
			c.Seg[DS] = sel
		}
		return nil
	case 0xC6, 0xC7: // MOV r/m, imm
		sz := c.opSizeFor(op)
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		imm, err := c.fetchImm(sz)
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		return c.wrap(ip, c.writeOp(m.rm, imm, sz), "寫回")
	case 0xC8: // ENTER
		size, err := c.fetch16()
		if err != nil {
			return c.wrap(ip, err, "enter size")
		}
		lvl, err := c.fetch8()
		if err != nil {
			return c.wrap(ip, err, "enter level")
		}
		if err := c.push16(c.R16(BP)); err != nil {
			return c.wrap(ip, err, "enter")
		}
		frame := c.R16(SP)
		for i := uint8(1); i < lvl; i++ {
			c.SetR16(BP, c.R16(BP)-2)
			v, err := c.Bus.ReadU16(c.Seg[SS], c.R16(BP))
			if err != nil {
				return c.wrap(ip, err, "enter 巢狀")
			}
			if err := c.push16(v); err != nil {
				return c.wrap(ip, err, "enter 巢狀")
			}
		}
		if lvl > 0 {
			if err := c.push16(frame); err != nil {
				return c.wrap(ip, err, "enter 巢狀")
			}
		}
		c.SetR16(BP, frame)
		c.SetR16(SP, c.R16(SP)-size)
		return nil
	case 0xC9: // LEAVE
		c.SetR16(SP, c.R16(BP))
		v, err := c.pop16()
		if err != nil {
			return c.wrap(ip, err, "leave")
		}
		c.SetR16(BP, v)
		return nil
	case 0xCA, 0xCB: // RETF
		var pop uint16
		if op == 0xCA {
			v, err := c.fetch16()
			if err != nil {
				return c.wrap(ip, err, "retf 立即數")
			}
			pop = v
		}
		return c.wrap(ip, c.RetFar(pop), "retf")
	case 0xCC:
		return c.errf(ip, "INT3（中斷點）")
	case 0xCD: // INT imm8
		n, err := c.fetch8()
		if err != nil {
			return c.wrap(ip, err, "int 號碼")
		}
		if c.OnInt != nil {
			handled, err := c.OnInt(c, n)
			if err != nil {
				return c.wrap(ip, err, "INT %02Xh", n)
			}
			if handled {
				return nil
			}
		}
		return c.errf(ip, "未實作的軟體中斷 INT %02Xh（AX=%04X）", n, c.R16(AX))
	case 0xD7: // XLAT
		v, err := c.Bus.ReadU8(c.dataSeg(DS), c.R16(BX)+uint16(c.Reg8(0)))
		if err != nil {
			return c.wrap(ip, err, "xlat")
		}
		c.SetReg8(0, v)
		return nil
	case 0xE0, 0xE1, 0xE2: // LOOPNE／LOOPE／LOOP
		d, err := c.fetch8()
		if err != nil {
			return c.wrap(ip, err, "取 rel8")
		}
		c.SetR16(CX, c.R16(CX)-1)
		take := c.R16(CX) != 0
		if op == 0xE0 {
			take = take && !c.Flag(FlagZF)
		} else if op == 0xE1 {
			take = take && c.Flag(FlagZF)
		}
		if take {
			c.IP += uint16(int16(int8(d)))
		}
		return nil
	case 0xE3: // JCXZ
		d, err := c.fetch8()
		if err != nil {
			return c.wrap(ip, err, "取 rel8")
		}
		if c.R16(CX) == 0 {
			c.IP += uint16(int16(int8(d)))
		}
		return nil
	case 0xE8: // CALL rel16
		d, err := c.fetch16()
		if err != nil {
			return c.wrap(ip, err, "取 rel16")
		}
		if err := c.push16(c.IP); err != nil {
			return c.wrap(ip, err, "call")
		}
		c.IP += d
		return nil
	case 0xE9: // JMP rel16
		d, err := c.fetch16()
		if err != nil {
			return c.wrap(ip, err, "取 rel16")
		}
		c.IP += d
		return nil
	case 0xEA: // JMP far imm
		off, err := c.fetch16()
		if err != nil {
			return c.wrap(ip, err, "far jmp 位移")
		}
		sel, err := c.fetch16()
		if err != nil {
			return c.wrap(ip, err, "far jmp selector")
		}
		return c.wrap(ip, c.farTransfer(sel, off, false), "far jmp")
	case 0xEB: // JMP rel8
		d, err := c.fetch8()
		if err != nil {
			return c.wrap(ip, err, "取 rel8")
		}
		c.IP += uint16(int16(int8(d)))
		return nil
	case 0xF4: // HLT
		c.Halt = true
		return nil
	case 0xF5: // CMC
		c.SetFlag(FlagCF, !c.Flag(FlagCF))
		return nil
	case 0xF6, 0xF7: // 群組 3
		return c.group3(c.opSizeFor(op), ip)
	case 0xF8:
		c.SetFlag(FlagCF, false)
		return nil
	case 0xF9:
		c.SetFlag(FlagCF, true)
		return nil
	case 0xFA:
		c.SetFlag(FlagIF, false)
		return nil
	case 0xFB:
		c.SetFlag(FlagIF, true)
		return nil
	case 0xFC:
		c.SetFlag(FlagDF, false)
		return nil
	case 0xFD:
		c.SetFlag(FlagDF, true)
		return nil
	case 0xFE, 0xFF: // 群組 4／5
		return c.group45(c.opSizeFor(op), ip)
	}
	return c.errf(ip, "未實作的 opcode %02X", op)
}

// exec0F 處理 0F 跳脫的雙位元組指令。這裡只收 386 的**應用層**幾條：
// 條件近程跳躍、零／符號延伸搬移、雙運算元 IMUL、SETcc。
// 保護模式的系統指令（LGDT 那一族）不做，碰到會帶位址停下。
func (c *CPU) exec0F(ip uint16) error {
	op, err := c.fetch8()
	if err != nil {
		return c.wrap(ip, err, "取 0F 次碼")
	}
	switch {
	case op >= 0x80 && op <= 0x8F: // Jcc rel16／rel32
		var d int32
		if c.opSize == S32 {
			v, err := c.fetch32()
			if err != nil {
				return c.wrap(ip, err, "取 rel32")
			}
			d = int32(v)
		} else {
			v, err := c.fetch16()
			if err != nil {
				return c.wrap(ip, err, "取 rel16")
			}
			d = int32(int16(v))
		}
		if c.cond(int(op - 0x80)) {
			c.IP += uint16(d)
		}
		return nil
	case op >= 0x90 && op <= 0x9F: // SETcc r/m8
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		v := uint32(0)
		if c.cond(int(op - 0x90)) {
			v = 1
		}
		return c.wrap(ip, c.writeOp(m.rm, v, S8), "寫回")
	}
	switch op {
	case 0x00: // 群組 6：SLDT／STR／LLDT／LTR／VERR／VERW
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		if m.reg != 4 && m.reg != 5 {
			return c.errf(ip, "未實作的 0F 00 /%d（LLDT／LTR 一族是系統指令）", m.reg)
		}
		// VERR／VERW：selector 可讀（可寫）就設 ZF。Borland 用它檢查
		// far 指標有沒有效，檢查不過就走錯誤分支。
		sel, err := c.readOp(m.rm, S16)
		if err != nil {
			return c.wrap(ip, err, "讀 selector")
		}
		ok := false
		if si, has := c.Bus.(SelectorInfo); has {
			_, ok = si.SelectorLimit(uint16(sel))
		}
		c.SetFlag(FlagZF, ok)
		return nil
	case 0x03: // LSL：載入段界限
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		sel, err := c.readOp(m.rm, S16)
		if err != nil {
			return c.wrap(ip, err, "讀 selector")
		}
		limit, ok := uint32(0), false
		if si, has := c.Bus.(SelectorInfo); has {
			limit, ok = si.SelectorLimit(uint16(sel))
		}
		c.SetFlag(FlagZF, ok)
		if ok {
			c.setReg(m.reg, limit, c.opSize)
		}
		return nil
	case 0xA4, 0xA5, 0xAC, 0xAD: // SHLD／SHRD
		// 雙精度位移：把另一個暫存器的位元從另一端「推進來」。
		// Borland 用它做 32 位元以上的移位。
		sz := c.opSize
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		var count uint32
		if op == 0xA4 || op == 0xAC {
			v, err := c.fetch8()
			if err != nil {
				return c.wrap(ip, err, "移位量")
			}
			count = uint32(v)
		} else {
			count = uint32(c.Reg8(1)) // CL
		}
		count &= 0x1F
		dst, err := c.readOp(m.rm, sz)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		src := c.reg(m.reg, sz)
		bits := widthBits(sz)
		if count == 0 {
			return nil
		}
		if count > bits {
			// 位移量超過位寬時結果未定義；停下來比裝作沒事好。
			return c.errf(ip, "SHLD/SHRD 的位移量 %d 超過位寬 %d（結果未定義）", count, bits)
		}
		m0 := widthMask(sz)
		var res uint32
		if op == 0xA4 || op == 0xA5 { // 左移
			res = (dst<<count | src>>(bits-count)) & m0
			c.SetFlag(FlagCF, dst>>(bits-count)&1 != 0)
		} else { // 右移
			res = (dst>>count | src<<(bits-count)) & m0
			c.SetFlag(FlagCF, dst>>(count-1)&1 != 0)
		}
		c.setSZP(res, sz)
		return c.wrap(ip, c.writeOp(m.rm, res, sz), "寫回")
	case 0xAF: // IMUL r, r/m
		sz := c.opSize
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		src, err := c.readOp(m.rm, sz)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		full := int64(signExtend(c.reg(m.reg, sz), sz)) * int64(signExtend(src, sz))
		c.setReg(m.reg, uint32(full), sz)
		over := full != int64(signExtend(uint32(full), sz))
		c.SetFlag(FlagCF, over)
		c.SetFlag(FlagOF, over)
		return nil
	case 0xB6, 0xB7, 0xBE, 0xBF: // MOVZX／MOVSX
		srcSz := S8
		if op&1 == 1 {
			srcSz = S16
		}
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		v, err := c.readOp(m.rm, srcSz)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		if op >= 0xBE {
			v = uint32(signExtend(v, srcSz))
		}
		c.setReg(m.reg, v, c.opSize)
		return nil
	}
	return c.errf(ip, "未實作的雙位元組 opcode 0F %02X", op)
}

func (c *CPU) group3(sz Size, ip uint16) error {
	m, err := c.decodeModRM()
	if err != nil {
		return c.wrap(ip, err, "ModRM")
	}
	a, err := c.readOp(m.rm, sz)
	if err != nil {
		return c.wrap(ip, err, "讀運算元")
	}
	switch m.reg {
	case 0, 1: // TEST r/m, imm
		imm, err := c.fetchImm(sz)
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		c.logic(a&imm, sz)
		return nil
	case 2: // NOT（不動旗標）
		return c.wrap(ip, c.writeOp(m.rm, ^a&widthMask(sz), sz), "寫回")
	case 3: // NEG
		res := c.sub(0, a, sz, 0)
		c.SetFlag(FlagCF, a&widthMask(sz) != 0)
		return c.wrap(ip, c.writeOp(m.rm, res, sz), "寫回")
	case 4: // MUL
		full := uint64(c.acc(sz)) * uint64(a&widthMask(sz))
		c.mulStore(full, sz)
		over := full>>widthBits(sz) != 0
		c.SetFlag(FlagCF, over)
		c.SetFlag(FlagOF, over)
		return nil
	case 5: // IMUL
		full := int64(signExtend(c.acc(sz), sz)) * int64(signExtend(a, sz))
		c.mulStore(uint64(full), sz)
		over := full != int64(signExtend(uint32(full)&widthMask(sz), sz))
		c.SetFlag(FlagCF, over)
		c.SetFlag(FlagOF, over)
		return nil
	case 6: // DIV
		d := uint64(a & widthMask(sz))
		if d == 0 {
			return c.errf(ip, "除以零")
		}
		num := c.divNum(sz)
		q, r := num/d, num%d
		if q > uint64(widthMask(sz)) {
			return c.errf(ip, "除法溢位（商 %d 超出 %d 位元）", q, widthBits(sz))
		}
		c.divStore(uint32(q), uint32(r), sz)
		return nil
	default: // IDIV
		d := int64(signExtend(a, sz))
		if d == 0 {
			return c.errf(ip, "除以零")
		}
		num := c.divNumSigned(sz)
		q, r := num/d, num%d
		if q != int64(signExtend(uint32(q)&widthMask(sz), sz)) {
			return c.errf(ip, "除法溢位（商 %d 超出 %d 位元）", q, widthBits(sz))
		}
		c.divStore(uint32(q), uint32(r), sz)
		return nil
	}
}

// mulStore 把乘積寫進 AX／DX:AX／EDX:EAX。
func (c *CPU) mulStore(full uint64, sz Size) {
	if sz == S8 {
		c.SetR16(AX, uint16(full))
		return
	}
	c.setReg(AX, uint32(full), sz)
	c.setReg(DX, uint32(full>>widthBits(sz)), sz)
}

// divNum 組出被除數：8 位元是 AX、16 位元是 DX:AX、32 位元是 EDX:EAX。
func (c *CPU) divNum(sz Size) uint64 {
	if sz == S8 {
		return uint64(c.R16(AX))
	}
	return uint64(c.reg(DX, sz))<<widthBits(sz) | uint64(c.reg(AX, sz))
}

func (c *CPU) divNumSigned(sz Size) int64 {
	if sz == S8 {
		return int64(int16(c.R16(AX)))
	}
	v := c.divNum(sz)
	if sz == S16 {
		return int64(int32(uint32(v)))
	}
	return int64(v)
}

// divStore 把商與餘數寫回：8 位元是 AL／AH，其餘是 acc／DX。
func (c *CPU) divStore(q, r uint32, sz Size) {
	if sz == S8 {
		c.SetReg8(0, uint8(q))
		c.SetReg8(4, uint8(r))
		return
	}
	c.setReg(AX, q, sz)
	c.setReg(DX, r, sz)
}

func (c *CPU) group45(sz Size, ip uint16) error {
	m, err := c.decodeModRM()
	if err != nil {
		return c.wrap(ip, err, "ModRM")
	}
	switch m.reg {
	case 0, 1: // INC／DEC
		a, err := c.readOp(m.rm, sz)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		var res uint32
		if m.reg == 0 {
			res = c.inc(a, sz)
		} else {
			res = c.dec(a, sz)
		}
		return c.wrap(ip, c.writeOp(m.rm, res, sz), "寫回")
	}
	if sz == S8 {
		return c.errf(ip, "群組 4（FE）只有 INC／DEC，收到 reg=%d", m.reg)
	}
	switch m.reg {
	case 2: // CALL near r/m
		v, err := c.readOp(m.rm, S16)
		if err != nil {
			return c.wrap(ip, err, "讀目標")
		}
		if err := c.push16(c.IP); err != nil {
			return c.wrap(ip, err, "call")
		}
		c.IP = uint16(v)
		return nil
	case 3, 5: // CALL／JMP far m16:16
		if m.rm.isReg {
			return c.errf(ip, "far call/jmp 的運算元是暫存器（無效編碼）")
		}
		off, err := c.Bus.ReadU16(m.rm.sel, m.rm.off)
		if err != nil {
			return c.wrap(ip, err, "讀 far 位移")
		}
		sel, err := c.Bus.ReadU16(m.rm.sel, m.rm.off+2)
		if err != nil {
			return c.wrap(ip, err, "讀 far selector")
		}
		return c.wrap(ip, c.farTransfer(sel, off, m.reg == 3), "far 轉移")
	case 4: // JMP near r/m
		v, err := c.readOp(m.rm, S16)
		if err != nil {
			return c.wrap(ip, err, "讀目標")
		}
		c.IP = uint16(v)
		return nil
	case 6: // PUSH r/m
		v, err := c.readOp(m.rm, sz)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		return c.wrap(ip, c.pushSize(v, sz), "push")
	}
	return c.errf(ip, "群組 5 的 reg=%d 未定義", m.reg)
}
