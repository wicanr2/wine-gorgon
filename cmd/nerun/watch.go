package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wicanr2/wine-gorgon/internal/cpu"
)

// setupWatch 把 `-watch` 的字串裝進 CPU 的觀察點表。
//
// 格式是逗號分隔的 `sel:off` 或 `sel:off=名字`，例如
// `002F:5F8F=A,0037:22D4=B`。API trace 只看得到跨模組的呼叫，
// 「模組內部是走哪一條分支到這裡」得靠這個。
//
// 命中時印一行：步數、位址、名字、以及當下的暫存器。**只印不停**——
// 要的是路徑，不是互動式除錯。
func setupWatch(c *cpu.CPU, spec string) error {
	if spec == "" {
		return nil
	}
	table := map[uint32]string{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name := part
		if i := strings.IndexByte(part, '='); i >= 0 {
			name, part = part[i+1:], part[:i]
		}
		sel, off, err := parseSelOff(part)
		if err != nil {
			return err
		}
		table[uint32(sel)<<16|uint32(off)] = name
	}
	c.Watch = table
	c.OnWatch = func(c *cpu.CPU, label string) {
		fmt.Printf("觀察點 #%-8d %04X:%04X %-20s AX=%04X BX=%04X CX=%04X DX=%04X SI=%04X DI=%04X DS=%04X ES=%04X SS:SP=%04X:%04X\n",
			c.Steps, c.Seg[cpu.CS], c.IP, label,
			c.R16(cpu.AX), c.R16(cpu.BX), c.R16(cpu.CX), c.R16(cpu.DX),
			c.R16(cpu.SI), c.R16(cpu.DI),
			c.Seg[cpu.DS], c.Seg[cpu.ES], c.Seg[cpu.SS], c.R16(cpu.SP))
	}
	return nil
}

func parseSelOff(s string) (sel, off uint16, err error) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return 0, 0, fmt.Errorf("觀察點 %q 要寫成 sel:off", s)
	}
	a, err := strconv.ParseUint(strings.TrimPrefix(s[:i], "0x"), 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("觀察點 %q 的 selector 不是十六進位：%w", s, err)
	}
	b, err := strconv.ParseUint(strings.TrimPrefix(s[i+1:], "0x"), 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("觀察點 %q 的位移不是十六進位：%w", s, err)
	}
	return uint16(a), uint16(b), nil
}
