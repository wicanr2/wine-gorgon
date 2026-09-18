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
//
//	watchmemuntil <stop> <上限> <out.jsonl> <memory> <length> [<memory> <length> …] [keepgoing]
//
// 多組範圍在**同一條 trace** 上觀測，每筆變更標 `range_index`；`keepgoing` 讓步數用盡
// 不中止整個腳本（見 cutKeepGoing）。收據尾筆列出這份觀測看不到什麼（notObserved）。
func watchMemoryUntil(p *win16.Process, args []string) error {
	args, keepGoing := cutKeepGoing(args)
	if len(args) < 5 {
		return fmt.Errorf("watchmemuntil 需要 target、limit、output，之後是一或多組 memory length，可再加 keepgoing")
	}
	target, err := parseWatchAddress(args[0])
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
	targets, err := parseWatchTargets(args[3:])
	if err != nil {
		return err
	}
	header := map[string]any{
		"schema": "wine-gorgon.memory-writes.v2",
		"target": target.String(), "limit": limit,
	}
	var rangeDoc []map[string]any
	for _, w := range targets {
		if w.prev, err = sampleProbe(p, w.addr, w.n); err != nil {
			return err
		}
		rangeDoc = append(rangeDoc, map[string]any{
			"index": w.index, "address": w.addr.String(), "length": w.n, "initial": w.prev})
	}
	header["ranges"] = rangeDoc
	// v1 只有一段，收據把它叫 `initial`；保留這個欄位，既有解讀程式不必改。
	header["initial"] = targets[0].prev
	f, err := os.Create(args[2])
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	if err = enc.Encode(header); err != nil {
		f.Close()
		return err
	}
	hits := 0
	byRange := make([]int, len(targets))
	observe := func() error {
		for _, w := range targets {
			current, e := sampleProbe(p, w.addr, w.n)
			if e != nil {
				return e
			}
			if current.MemoryHex != w.prev.MemoryHex {
				hits++
				byRange[w.index]++
				if e = enc.Encode(map[string]any{
					"range_index": w.index, "range_address": w.addr.String(),
					"before_step": w.prev, "after_step": current,
					"single_step": current.Steps == w.prev.Steps+1}); e != nil {
					return e
				}
			}
			w.prev = current
		}
		return nil
	}
	startSteps := p.CPU.Steps
	runErr := runUntil(p, target, limit, observe)
	// 目標指令前的最後一次寫入也要收錄；逾限／HLT 仍只留下不完整收據。
	if e := observe(); runErr == nil {
		runErr = e
	}
	// 步數用盡與 CPU 結束要分開講：前者是「看完這段了」，後者是程式真的跑完了。
	// 用 runUntil 之外的事實判斷，不靠比對錯誤字串。
	exhausted := runErr != nil && !p.CPU.Halt && p.CPU.Steps-startSteps >= limit
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	tailErr := enc.Encode(map[string]any{
		"complete": runErr == nil, "hits": hits, "end_steps": p.CPU.Steps, "error": message,
		"steps_exhausted": exhausted, "cpu_halted": p.CPU.Halt,
		"hits_by_range": byRange, "not_observed": notObserved(exhausted)})
	closeErr := f.Close()
	if runErr != nil {
		if exhausted && keepGoing {
			// 明講一句再往下跑：**沉默地繼續**會讓截斷的觀測看起來像完整的。
			fmt.Printf("watchmemuntil %s：%v；keepgoing → 繼續下一行（收據 complete=false）\n",
				args[2], runErr)
			return nil
		}
		return runErr
	}
	if tailErr != nil {
		return tailErr
	}
	return closeErr
}
