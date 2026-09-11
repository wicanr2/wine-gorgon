package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wicanr2/wine-gorgon/internal/cpu"
	"github.com/wicanr2/wine-gorgon/internal/win16"
	"github.com/wicanr2/wine-gorgon/internal/winapi"
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

// setupMemWatch 裝上「誰寫了這塊記憶體」的監看。
//
// 格式是 `sel:off` 或 `sel:off+長度`（長度十進位，預設 1）。畫面是遊戲
// 自己寫進 DIB 的，所以要找某個像素是哪一段程式碼畫的，只能從寫入端追。
// 印出來的 CS:IP 就是下一步拿去反組譯的位址。
func setupMemWatch(p *win16.Process, spec string, limit int, dumpDir string) error {
	c := p.CPU
	if spec == "" {
		return nil
	}
	length := 1
	if i := strings.IndexByte(spec, '+'); i >= 0 {
		n, err := strconv.Atoi(spec[i+1:])
		if err != nil || n <= 0 {
			return fmt.Errorf("記憶體監看 %q 的長度要是正整數", spec)
		}
		length, spec = n, spec[:i]
	}
	sel, off, err := parseSelOff(spec)
	if err != nil {
		return err
	}
	lo, hi := uint32(off), uint32(off)+uint32(length)
	seen := map[uint32]int{}
	shown := 0
	c.OnMemWrite = func(c *cpu.CPU, s uint16, o uint32, size int, v uint32) {
		if s != sel || o+uint32(size) <= lo || o >= hi {
			return
		}
		// 同一個 (CS:IP, 來源) 只印一次——繪圖是迴圈，全印會淹掉；
		// 但**來源不同就要印**：暫存緩衝區會被不同的素材輪流用，
		// 只認 CS:IP 會把後面那些不同的來源全部吃掉。
		key := uint32(c.Seg[cpu.CS])<<16 ^ uint32(c.IP) ^ uint32(c.Seg[cpu.DS])<<8 ^ c.R[cpu.SI]
		seen[key]++
		if seen[key] > 1 || shown >= limit {
			return
		}
		shown++
		// 一併印來源指標：老遊戲的貼圖迴圈是 DS:ESI → ES:EDI，
		// 找「這個像素從哪來」時，來源位址比目的位址有用。
		src := c.Seg[cpu.DS]
		fmt.Printf("記憶體監看 #%-8d %04X:%04X 寫 %04X:%04X 長度 %d 值 %X　來源 DS:ESI=%04X:%08X\n",
			c.Steps, c.Seg[cpu.CS], c.IP, s, o, size, v, src, c.R[cpu.SI])
		// 來源那一塊通常是暫時的（載入 → 貼圖 → 釋放），等腳本跑完再問就
		// 已經不在了。命中的當下就整塊存出來。
		if dumpDir == "" {
			return
		}
		blk, ok := p.Mod.Mem.Block(src)
		if !ok {
			return
		}
		name := fmt.Sprintf("%s/src-%04X-%d.bin", dumpDir, src, c.Steps)
		if err := os.WriteFile(name, blk.Data, 0o644); err != nil {
			fmt.Printf("  來源區塊寫檔失敗：%v\n", err)
			return
		}
		fmt.Printf("  來源區塊 %q %d bytes → %s\n", blk.Name, len(blk.Data), name)
	}
	// Go 這一側的寫入（hmemcpy／_lread／BitBlt）不經過 CPU，另外掛一層。
	p.Mod.Mem.OnWrite = func(s uint16, o, n int, how string) {
		if s != sel || uint32(o+n) <= lo || uint32(o) >= hi {
			return
		}
		api := "?"
		if len(p.Trace) > 0 {
			api = winapi.Describe(p.Trace[len(p.Trace)-1].Import.Key())
		}
		key := uint32(0x8000_0000) | uint32(o)
		seen[key]++
		if seen[key] > 1 || shown >= limit {
			return
		}
		shown++
		fmt.Printf("記憶體監看 #%-8d API 寫 %04X:%04X 長度 %d（%s，最近一次 API＝%s）\n",
			c.Steps, s, o, n, how, api)
	}
	return nil
}
