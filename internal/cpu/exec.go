package cpu

// Step 執行一條指令（含前綴）。
func (c *CPU) Step() error {
	c.segOverride = -1
	c.repPrefix = 0
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
	c.R[SP] += popBytes
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

func (c *CPU) exec(op uint8, ip uint16) error {
	// --- 0x00..0x3F：八個 ALU 運算 ＋ 段暫存器推入彈出 ---
	if op < 0x40 {
		grp := int(op >> 3)
		switch op & 7 {
		case 0, 1: // op r/m, r
			w := op&1 == 1
			m, err := c.decodeModRM()
			if err != nil {
				return c.wrap(ip, err, "ModRM")
			}
			a, err := c.readW(m.rm, w)
			if err != nil {
				return c.wrap(ip, err, "讀運算元")
			}
			b := c.regW(m.reg, w)
			res, store := c.aluOp(grp, a, b, w)
			if store {
				return c.wrap(ip, c.writeW(m.rm, res, w), "寫回")
			}
			return nil
		case 2, 3: // op r, r/m
			w := op&1 == 1
			m, err := c.decodeModRM()
			if err != nil {
				return c.wrap(ip, err, "ModRM")
			}
			b, err := c.readW(m.rm, w)
			if err != nil {
				return c.wrap(ip, err, "讀運算元")
			}
			res, store := c.aluOp(grp, c.regW(m.reg, w), b, w)
			if store {
				c.setRegW(m.reg, res, w)
			}
			return nil
		case 4, 5: // op acc, imm
			w := op&1 == 1
			imm, err := c.immW(w)
			if err != nil {
				return c.wrap(ip, err, "立即數")
			}
			res, store := c.aluOp(grp, c.acc(w), imm, w)
			if store {
				c.setAcc(res, w)
			}
			return nil
		case 6: // push sreg（0x0E 是 push cs）
			if grp > 3 {
				return c.errf(ip, "未實作的 opcode %02X", op)
			}
			return c.wrap(ip, c.push16(c.Seg[grp]), "push 段暫存器")
		default: // pop sreg；0x0F 在 286 是雙位元組跳脫，不是 pop cs
			if op == 0x0F {
				nxt, _ := c.Bus.ReadU8(c.Seg[CS], c.IP)
				return c.errf(ip, "未實作的雙位元組 opcode 0F %02X（286 系統指令？）", nxt)
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
	case op >= 0x40 && op <= 0x47: // INC r16
		c.R[op-0x40] = uint16(c.inc(uint32(c.R[op-0x40]), true))
		return nil
	case op >= 0x48 && op <= 0x4F: // DEC r16
		c.R[op-0x48] = uint16(c.dec(uint32(c.R[op-0x48]), true))
		return nil
	case op >= 0x50 && op <= 0x57: // PUSH r16
		return c.wrap(ip, c.push16(c.R[op-0x50]), "push")
	case op >= 0x58 && op <= 0x5F: // POP r16
		v, err := c.pop16()
		if err != nil {
			return c.wrap(ip, err, "pop")
		}
		c.R[op-0x58] = v
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
	case op >= 0x91 && op <= 0x97: // XCHG AX, r16
		r := int(op - 0x90)
		c.R[AX], c.R[r] = c.R[r], c.R[AX]
		return nil
	case op >= 0xB0 && op <= 0xB7: // MOV r8, imm8
		v, err := c.fetch8()
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		c.SetReg8(int(op-0xB0), v)
		return nil
	case op >= 0xB8 && op <= 0xBF: // MOV r16, imm16
		v, err := c.fetch16()
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		c.R[op-0xB8] = v
		return nil
	}

	switch op {
	case 0x60: // PUSHA
		sp := c.R[SP]
		for i := 0; i < 8; i++ {
			v := c.R[i]
			if i == SP {
				v = sp
			}
			if err := c.push16(v); err != nil {
				return c.wrap(ip, err, "pusha")
			}
		}
		return nil
	case 0x61: // POPA（SP 那一格丟掉）
		for i := 7; i >= 0; i-- {
			v, err := c.pop16()
			if err != nil {
				return c.wrap(ip, err, "popa")
			}
			if i != SP {
				c.R[i] = v
			}
		}
		return nil
	case 0x68: // PUSH imm16
		v, err := c.fetch16()
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		return c.wrap(ip, c.push16(v), "push")
	case 0x6A: // PUSH imm8（符號延伸）
		v, err := c.fetch8()
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		return c.wrap(ip, c.push16(uint16(int16(int8(v)))), "push")
	case 0x69, 0x6B: // IMUL r16, r/m16, imm
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		src, err := c.read16(m.rm)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		var imm int32
		if op == 0x69 {
			v, err := c.fetch16()
			if err != nil {
				return c.wrap(ip, err, "立即數")
			}
			imm = int32(int16(v))
		} else {
			v, err := c.fetch8()
			if err != nil {
				return c.wrap(ip, err, "立即數")
			}
			imm = int32(int8(v))
		}
		full := int32(int16(src)) * imm
		c.R[m.reg] = uint16(full)
		over := full != int32(int16(full))
		c.SetFlag(FlagCF, over)
		c.SetFlag(FlagOF, over)
		return nil
	case 0x80, 0x81, 0x82, 0x83: // 群組 1：op r/m, imm
		w := op&1 == 1
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		var imm uint32
		if op == 0x81 {
			v, err := c.fetch16()
			if err != nil {
				return c.wrap(ip, err, "立即數")
			}
			imm = uint32(v)
		} else {
			v, err := c.fetch8()
			if err != nil {
				return c.wrap(ip, err, "立即數")
			}
			if op == 0x83 {
				imm = uint32(uint16(int16(int8(v)))) // 0x83 是符號延伸到 16 位元
			} else {
				imm = uint32(v)
			}
		}
		a, err := c.readW(m.rm, w)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		res, store := c.aluOp(m.reg, a, imm, w)
		if store {
			return c.wrap(ip, c.writeW(m.rm, res, w), "寫回")
		}
		return nil
	case 0x84, 0x85: // TEST r/m, r
		w := op&1 == 1
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		a, err := c.readW(m.rm, w)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		c.logic(a&c.regW(m.reg, w), w)
		return nil
	case 0x86, 0x87: // XCHG r/m, r
		w := op&1 == 1
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		a, err := c.readW(m.rm, w)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		b := c.regW(m.reg, w)
		if err := c.writeW(m.rm, b, w); err != nil {
			return c.wrap(ip, err, "寫回")
		}
		c.setRegW(m.reg, a, w)
		return nil
	case 0x88, 0x89: // MOV r/m, r
		w := op&1 == 1
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		return c.wrap(ip, c.writeW(m.rm, c.regW(m.reg, w), w), "寫回")
	case 0x8A, 0x8B: // MOV r, r/m
		w := op&1 == 1
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		v, err := c.readW(m.rm, w)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		c.setRegW(m.reg, v, w)
		return nil
	case 0x8C: // MOV r/m16, sreg
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		return c.wrap(ip, c.write16(m.rm, c.Seg[m.reg&3]), "寫回")
	case 0x8D: // LEA：只算位址，不碰記憶體
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		if m.rm.isReg {
			return c.errf(ip, "LEA 的運算元是暫存器（無效編碼）")
		}
		c.R[m.reg] = m.rm.off
		return nil
	case 0x8E: // MOV sreg, r/m16
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		v, err := c.read16(m.rm)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		c.Seg[m.reg&3] = v
		return nil
	case 0x8F: // POP r/m16
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		v, err := c.pop16()
		if err != nil {
			return c.wrap(ip, err, "pop")
		}
		return c.wrap(ip, c.write16(m.rm, v), "寫回")
	case 0x90: // NOP
		return nil
	case 0x98: // CBW
		c.R[AX] = uint16(int16(int8(c.Reg8(0))))
		return nil
	case 0x99: // CWD
		if c.R[AX]&0x8000 != 0 {
			c.R[DX] = 0xFFFF
		} else {
			c.R[DX] = 0
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
		w := op&1 == 1
		d, err := c.fetch16()
		if err != nil {
			return c.wrap(ip, err, "位移")
		}
		o := operand{sel: c.dataSeg(DS), off: d}
		if op < 0xA2 {
			v, err := c.readW(o, w)
			if err != nil {
				return c.wrap(ip, err, "讀運算元")
			}
			c.setAcc(v, w)
			return nil
		}
		return c.wrap(ip, c.writeW(o, c.acc(w), w), "寫回")
	case 0xA4, 0xA5, 0xA6, 0xA7, 0xAA, 0xAB, 0xAC, 0xAD, 0xAE, 0xAF:
		return c.wrap(ip, c.stringOp(op&^1, op&1 == 1), "字串指令")
	case 0xA8, 0xA9: // TEST acc, imm
		w := op&1 == 1
		imm, err := c.immW(w)
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		c.logic(c.acc(w)&imm, w)
		return nil
	case 0xC0, 0xC1, 0xD0, 0xD1, 0xD2, 0xD3: // 群組 2：位移／旋轉
		w := op&1 == 1
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
		a, err := c.readW(m.rm, w)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		return c.wrap(ip, c.writeW(m.rm, c.shiftOp(m.reg, a, count, w), w), "寫回")
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
		c.R[SP] += pop
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
		c.R[m.reg] = off
		if op == 0xC4 {
			c.Seg[ES] = sel
		} else {
			c.Seg[DS] = sel
		}
		return nil
	case 0xC6, 0xC7: // MOV r/m, imm
		w := op&1 == 1
		m, err := c.decodeModRM()
		if err != nil {
			return c.wrap(ip, err, "ModRM")
		}
		imm, err := c.immW(w)
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		return c.wrap(ip, c.writeW(m.rm, imm, w), "寫回")
	case 0xC8: // ENTER
		size, err := c.fetch16()
		if err != nil {
			return c.wrap(ip, err, "enter size")
		}
		lvl, err := c.fetch8()
		if err != nil {
			return c.wrap(ip, err, "enter level")
		}
		if err := c.push16(c.R[BP]); err != nil {
			return c.wrap(ip, err, "enter")
		}
		frame := c.R[SP]
		for i := uint8(1); i < lvl; i++ {
			c.R[BP] -= 2
			v, err := c.Bus.ReadU16(c.Seg[SS], c.R[BP])
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
		c.R[BP] = frame
		c.R[SP] -= size
		return nil
	case 0xC9: // LEAVE
		c.R[SP] = c.R[BP]
		v, err := c.pop16()
		if err != nil {
			return c.wrap(ip, err, "leave")
		}
		c.R[BP] = v
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
	case 0xCD: // INT imm8
		n, err := c.fetch8()
		if err != nil {
			return c.wrap(ip, err, "int 號碼")
		}
		return c.errf(ip, "未實作的軟體中斷 INT %02Xh", n)
	case 0xCC:
		return c.errf(ip, "INT3（中斷點）")
	case 0xD7: // XLAT
		v, err := c.Bus.ReadU8(c.dataSeg(DS), c.R[BX]+uint16(c.Reg8(0)))
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
		c.R[CX]--
		take := c.R[CX] != 0
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
		if c.R[CX] == 0 {
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
		return c.group3(op&1 == 1, ip)
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
		return c.group45(op&1 == 1, ip)
	}
	return c.errf(ip, "未實作的 opcode %02X", op)
}

func (c *CPU) group3(w bool, ip uint16) error {
	m, err := c.decodeModRM()
	if err != nil {
		return c.wrap(ip, err, "ModRM")
	}
	a, err := c.readW(m.rm, w)
	if err != nil {
		return c.wrap(ip, err, "讀運算元")
	}
	switch m.reg {
	case 0, 1: // TEST r/m, imm
		imm, err := c.immW(w)
		if err != nil {
			return c.wrap(ip, err, "立即數")
		}
		c.logic(a&imm, w)
		return nil
	case 2: // NOT（不動旗標）
		return c.wrap(ip, c.writeW(m.rm, ^a&widthMask(w), w), "寫回")
	case 3: // NEG
		res := c.sub(0, a, w, 0)
		c.SetFlag(FlagCF, a&widthMask(w) != 0)
		return c.wrap(ip, c.writeW(m.rm, res, w), "寫回")
	case 4: // MUL
		if w {
			full := uint32(c.R[AX]) * a
			c.R[AX], c.R[DX] = uint16(full), uint16(full>>16)
			over := c.R[DX] != 0
			c.SetFlag(FlagCF, over)
			c.SetFlag(FlagOF, over)
		} else {
			full := uint32(c.Reg8(0)) * a
			c.R[AX] = uint16(full)
			over := full&0xFF00 != 0
			c.SetFlag(FlagCF, over)
			c.SetFlag(FlagOF, over)
		}
		return nil
	case 5: // IMUL
		if w {
			full := int32(int16(c.R[AX])) * int32(int16(a))
			c.R[AX], c.R[DX] = uint16(full), uint16(full>>16)
			over := full != int32(int16(full))
			c.SetFlag(FlagCF, over)
			c.SetFlag(FlagOF, over)
		} else {
			full := int16(int8(c.Reg8(0))) * int16(int8(a))
			c.R[AX] = uint16(full)
			over := full != int16(int8(full))
			c.SetFlag(FlagCF, over)
			c.SetFlag(FlagOF, over)
		}
		return nil
	case 6: // DIV
		if a == 0 {
			return c.errf(ip, "除以零")
		}
		if w {
			num := uint32(c.R[DX])<<16 | uint32(c.R[AX])
			q := num / a
			if q > 0xFFFF {
				return c.errf(ip, "除法溢位（商 %d 超出 16 位元）", q)
			}
			c.R[AX], c.R[DX] = uint16(q), uint16(num%a)
		} else {
			num := uint32(c.R[AX])
			q := num / a
			if q > 0xFF {
				return c.errf(ip, "除法溢位（商 %d 超出 8 位元）", q)
			}
			c.SetReg8(0, uint8(q))
			c.SetReg8(4, uint8(num%a))
		}
		return nil
	default: // IDIV
		if a == 0 {
			return c.errf(ip, "除以零")
		}
		if w {
			num := int32(uint32(c.R[DX])<<16 | uint32(c.R[AX]))
			d := int32(int16(a))
			q, r := num/d, num%d
			if q != int32(int16(q)) {
				return c.errf(ip, "除法溢位（商 %d 超出 16 位元）", q)
			}
			c.R[AX], c.R[DX] = uint16(q), uint16(r)
		} else {
			num := int16(c.R[AX])
			d := int16(int8(a))
			q, r := num/d, num%d
			if q != int16(int8(q)) {
				return c.errf(ip, "除法溢位（商 %d 超出 8 位元）", q)
			}
			c.SetReg8(0, uint8(q))
			c.SetReg8(4, uint8(r))
		}
		return nil
	}
}

func (c *CPU) group45(w bool, ip uint16) error {
	m, err := c.decodeModRM()
	if err != nil {
		return c.wrap(ip, err, "ModRM")
	}
	switch m.reg {
	case 0, 1: // INC／DEC
		a, err := c.readW(m.rm, w)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		var res uint32
		if m.reg == 0 {
			res = c.inc(a, w)
		} else {
			res = c.dec(a, w)
		}
		return c.wrap(ip, c.writeW(m.rm, res, w), "寫回")
	}
	if !w {
		return c.errf(ip, "群組 4（FE）只有 INC／DEC，收到 reg=%d", m.reg)
	}
	switch m.reg {
	case 2: // CALL near r/m
		v, err := c.read16(m.rm)
		if err != nil {
			return c.wrap(ip, err, "讀目標")
		}
		if err := c.push16(c.IP); err != nil {
			return c.wrap(ip, err, "call")
		}
		c.IP = v
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
		v, err := c.read16(m.rm)
		if err != nil {
			return c.wrap(ip, err, "讀目標")
		}
		c.IP = v
		return nil
	case 6: // PUSH r/m16
		v, err := c.read16(m.rm)
		if err != nil {
			return c.wrap(ip, err, "讀運算元")
		}
		return c.wrap(ip, c.push16(v), "push")
	}
	return c.errf(ip, "群組 5 的 reg=%d 未定義", m.reg)
}

// --- 依位寬取用的小工具 ---

func (c *CPU) readW(o operand, w bool) (uint32, error) {
	if w {
		v, err := c.read16(o)
		return uint32(v), err
	}
	v, err := c.read8(o)
	return uint32(v), err
}

func (c *CPU) writeW(o operand, v uint32, w bool) error {
	if w {
		return c.write16(o, uint16(v))
	}
	return c.write8(o, uint8(v))
}

func (c *CPU) regW(i int, w bool) uint32 {
	if w {
		return uint32(c.R[i])
	}
	return uint32(c.Reg8(i))
}

func (c *CPU) setRegW(i int, v uint32, w bool) {
	if w {
		c.R[i] = uint16(v)
		return
	}
	c.SetReg8(i, uint8(v))
}

func (c *CPU) immW(w bool) (uint32, error) {
	if w {
		v, err := c.fetch16()
		return uint32(v), err
	}
	v, err := c.fetch8()
	return uint32(v), err
}
