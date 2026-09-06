package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wicanr2/wine-gorgon/internal/cpu"
	"github.com/wicanr2/wine-gorgon/internal/win16"
)

type probeAddress struct{ Sel, Off uint16 }

func parseProbeAddress(s string) (probeAddress, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return probeAddress{}, fmt.Errorf("位址必須為 selector:offset：%q", s)
	}
	sel, e := strconv.ParseUint(parts[0], 16, 16)
	if e != nil {
		return probeAddress{}, e
	}
	off, e := strconv.ParseUint(parts[1], 16, 16)
	if e != nil {
		return probeAddress{}, e
	}
	return probeAddress{uint16(sel), uint16(off)}, nil
}
func (a probeAddress) String() string { return fmt.Sprintf("%04X:%04X", a.Sel, a.Off) }
func probeBytes(p *win16.Process, a probeAddress, n uint64) ([]byte, error) {
	if n == 0 || n > 65536 || uint64(a.Off)+n > 65536 {
		return nil, fmt.Errorf("記憶體範圍不可為空或跨越 64 KiB")
	}
	b, ok := p.Mod.Mem.Block(a.Sel)
	if !ok || uint64(a.Off)+n > uint64(len(b.Data)) {
		return nil, fmt.Errorf("未配置或超出區塊：%s 長度 %d", a, n)
	}
	return b.Data[int(a.Off) : int(a.Off)+int(n)], nil
}

type probeState struct {
	Steps         uint64    `json:"steps"`
	CSIP          string    `json:"cs_ip"`
	Registers     [8]uint32 `json:"registers"`
	Segments      [4]uint16 `json:"segments"`
	Flags         uint16    `json:"flags"`
	MemoryAddress string    `json:"memory_address"`
	MemoryHex     string    `json:"memory_hex"`
}

func sampleProbe(p *win16.Process, a probeAddress, n uint64) (probeState, error) {
	b, e := probeBytes(p, a, n)
	if e != nil {
		return probeState{}, e
	}
	c := p.CPU
	return probeState{c.Steps, fmt.Sprintf("%04X:%04X", c.Seg[cpu.CS], c.IP), c.R, c.Seg, c.Flags, a.String(), hex.EncodeToString(b)}, nil
}

// runUntil 在目標指令執行之前停止；步數用盡、CPU 結束與錯誤都不能算命中。
// 不跳指令、不替換亂數，不改 CPU／API 的正式執行路徑。
func runUntil(p *win16.Process, target probeAddress, limit uint64, watch func() error) error {
	if limit == 0 {
		return fmt.Errorf("步數上限必須大於零")
	}
	start := p.CPU.Steps
	for {
		if p.CPU.Seg[cpu.CS] == target.Sel && p.CPU.IP == target.Off {
			return nil
		}
		if p.CPU.Halt {
			return fmt.Errorf("CPU 已結束，未命中 %s", target)
		}
		if p.CPU.Steps-start >= limit {
			return fmt.Errorf("用盡 %d 條指令，未命中 %s", limit, target)
		}
		if watch != nil {
			if e := watch(); e != nil {
				return e
			}
		}
		if e := p.CPU.Step(); e != nil {
			return e
		}
	}
}

// poke 採比較後寫入：預期 bytes 不符或範圍錯誤時，一個 byte 都不改。
// traceuntil 有界串流寫 JSONL；失敗尾筆保留 complete=false，不能當成功收據。
func probeCommand(p *win16.Process, cmd string, args []string, echo func(string)) (bool, error) {
	arity := map[string]int{"until": 2, "poke": 3, "state": 3, "traceuntil": 6}
	count, ok := arity[cmd]
	if !ok {
		return false, nil
	}
	if len(args) != count {
		return true, fmt.Errorf("%s 需要 %d 個參數", cmd, count)
	}
	if cmd == "poke" {
		a, e := parseProbeAddress(args[0])
		if e != nil {
			return true, e
		}
		old, e := hex.DecodeString(args[1])
		if e != nil {
			return true, e
		}
		next, e := hex.DecodeString(args[2])
		if e != nil {
			return true, e
		}
		if len(old) != len(next) {
			return true, fmt.Errorf("預期值與新值長度必須相同")
		}
		b, e := probeBytes(p, a, uint64(len(old)))
		if e != nil {
			return true, e
		}
		if !bytes.Equal(b, old) {
			return true, fmt.Errorf("%s 預期 %x，實際 %x；未寫入", a, old, b)
		}
		copy(b, next)
		echo(fmt.Sprintf("probe-injection %s %x -> %x at step %d", a, old, next, p.CPU.Steps))
		return true, nil
	}
	if cmd == "state" {
		a, e := parseProbeAddress(args[1])
		if e != nil {
			return true, e
		}
		n, e := strconv.ParseUint(args[2], 0, 64)
		if e != nil {
			return true, e
		}
		st, e := sampleProbe(p, a, n)
		if e != nil {
			return true, e
		}
		raw, e := json.MarshalIndent(struct {
			Schema string     `json:"schema"`
			State  probeState `json:"state"`
		}{"wine-gorgon.state.v1", st}, "", "  ")
		if e != nil {
			return true, e
		}
		return true, os.WriteFile(args[0], append(raw, '\n'), 0644)
	}
	target, e := parseProbeAddress(args[0])
	if e != nil {
		return true, e
	}
	limit, e := strconv.ParseUint(args[1], 0, 64)
	if e != nil {
		return true, e
	}
	if cmd == "until" {
		e = runUntil(p, target, limit, nil)
		if e == nil {
			echo(fmt.Sprintf("until %s 命中於 step %d", target, p.CPU.Steps))
		}
		return true, e
	}
	watched, e := parseProbeAddress(args[2])
	if e != nil {
		return true, e
	}
	mem, e := parseProbeAddress(args[4])
	if e != nil {
		return true, e
	}
	n, e := strconv.ParseUint(args[5], 0, 64)
	if e != nil {
		return true, e
	}
	if _, e = probeBytes(p, mem, n); e != nil {
		return true, e
	}
	if limit == 0 {
		return true, fmt.Errorf("步數上限必須大於零")
	}
	f, e := os.Create(args[3])
	if e != nil {
		return true, e
	}
	enc := json.NewEncoder(f)
	if e = enc.Encode(map[string]any{"schema": "wine-gorgon.trace.v1", "target": target.String(), "watch": watched.String(), "limit": limit, "start_steps": p.CPU.Steps}); e != nil {
		f.Close()
		return true, e
	}
	hits := uint64(0)
	e = runUntil(p, target, limit, func() error {
		if p.CPU.Seg[cpu.CS] != watched.Sel || p.CPU.IP != watched.Off {
			return nil
		}
		hits++
		st, e := sampleProbe(p, mem, n)
		if e != nil {
			return e
		}
		return enc.Encode(st)
	})
	message := ""
	if e != nil {
		message = e.Error()
	}
	tailErr := enc.Encode(map[string]any{"complete": e == nil, "hits": hits, "end_steps": p.CPU.Steps, "error": message})
	closeErr := f.Close()
	if e != nil {
		return true, e
	}
	if tailErr != nil {
		return true, tailErr
	}
	return true, closeErr
}
