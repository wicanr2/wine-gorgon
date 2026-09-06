package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wicanr2/wine-gorgon/internal/cpu"
	"github.com/wicanr2/wine-gorgon/internal/win16"
)

// setProbeRegister 明示注入 16-bit CPU 狀態；compare-before-write，保留高 16 bits。
func setProbeRegister(p *win16.Process, args []string, echo func(string)) error {
	if len(args) != 3 {
		return fmt.Errorf("reg 需要名稱、預期值與新值（16-bit 十六進位）")
	}
	name := strings.ToLower(args[0])
	old, err := strconv.ParseUint(args[1], 16, 16)
	if err != nil {
		return err
	}
	next, err := strconv.ParseUint(args[2], 16, 16)
	if err != nil {
		return err
	}
	indices := map[string]int{"ax": cpu.AX, "cx": cpu.CX, "dx": cpu.DX, "bx": cpu.BX, "sp": cpu.SP, "bp": cpu.BP, "si": cpu.SI, "di": cpu.DI}
	i, ok := indices[name]
	if !ok && name != "ip" {
		return fmt.Errorf("不支援的暫存器 %q", name)
	}
	current := uint16(p.CPU.IP)
	if ok {
		current = uint16(p.CPU.R[i])
	}
	if current != uint16(old) {
		return fmt.Errorf("%s 預期 %04X，實際 %04X；未寫入", name, old, current)
	}
	if name == "ip" {
		if _, err := probeBytes(p, probeAddress{p.CPU.Seg[cpu.CS], uint16(next)}, 1); err != nil {
			return err
		}
	}
	if name == "sp" {
		if _, err := probeBytes(p, probeAddress{p.CPU.Seg[cpu.SS], uint16(next)}, 1); err != nil {
			return err
		}
	}
	before := fmt.Sprintf("%04X:%04X", p.CPU.Seg[cpu.CS], p.CPU.IP)
	if ok {
		p.CPU.R[i] = (p.CPU.R[i] & 0xffff0000) | uint32(next)
	} else {
		p.CPU.IP = uint16(next)
	}
	echo(fmt.Sprintf("probe-injection register %s %04X -> %04X at %s step %d", name, old, next, before, p.CPU.Steps))
	return nil
}

// watchMemoryUntil 記錄每個外層 Step 的記憶體變更，不把巢狀 Win16 callback 冒稱單指令 writer。
func watchMemoryUntil(p *win16.Process, args []string) error {
	if len(args) != 5 {
		return fmt.Errorf("watchmemuntil 需要 target、limit、output、memory、length")
	}
	target, err := parseProbeAddress(args[0])
	if err != nil {
		return err
	}
	limit, err := strconv.ParseUint(args[1], 0, 64)
	if err != nil {
		return err
	}
	if limit == 0 {
		return fmt.Errorf("步數上限必須大於零")
	}
	mem, err := parseProbeAddress(args[3])
	if err != nil {
		return err
	}
	n, err := strconv.ParseUint(args[4], 0, 64)
	if err != nil {
		return err
	}
	previous, err := sampleProbe(p, mem, n)
	if err != nil {
		return err
	}
	f, err := os.Create(args[2])
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	if err = enc.Encode(map[string]any{"schema": "wine-gorgon.memory-writes.v1", "target": target.String(), "limit": limit, "initial": previous}); err != nil {
		f.Close()
		return err
	}
	hits := 0
	observe := func() error {
		current, e := sampleProbe(p, mem, n)
		if e != nil {
			return e
		}
		if current.MemoryHex != previous.MemoryHex {
			hits++
			if e = enc.Encode(map[string]any{"before_step": previous, "after_step": current, "single_step": current.Steps == previous.Steps+1}); e != nil {
				return e
			}
		}
		previous = current
		return nil
	}
	runErr := runUntil(p, target, limit, observe)
	// 目標指令前的最後一次寫入也要收錄；逾限／HLT 仍只留下不完整收據。
	if e := observe(); runErr == nil {
		runErr = e
	}
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	tailErr := enc.Encode(map[string]any{"complete": runErr == nil, "hits": hits, "end_steps": p.CPU.Steps, "error": message})
	closeErr := f.Close()
	if runErr != nil {
		return runErr
	}
	if tailErr != nil {
		return tailErr
	}
	return closeErr
}
