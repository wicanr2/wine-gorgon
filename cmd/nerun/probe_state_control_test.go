package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wine-gorgon/internal/cpu"
)

func TestProbeRegisterInjectionIsAtomic(t *testing.T) {
	p := probeProcess([]byte{0x90, 0x90, 0xf4})
	p.CPU.R[cpu.SI] = 0xabcd1234
	p.CPU.Seg[cpu.SS] = 23
	quiet := func(string) {}
	for _, line := range []string{"reg si 0000 0003", "reg nope 0000 0001", "reg si 1234 10000", "reg ip 0000 ffff", "reg sp 0000 0008"} {
		before := p.CPU.R
		ip := p.CPU.IP
		if err := runScriptLine(p, line, quiet); err == nil {
			t.Fatalf("必須拒絕 %s", line)
		}
		if before != p.CPU.R || ip != p.CPU.IP {
			t.Fatal("失敗發生部分修改")
		}
	}
	var log string
	if err := runScriptLine(p, "reg si 1234 0003", func(s string) { log = s }); err != nil {
		t.Fatal(err)
	}
	if p.CPU.R[cpu.SI] != 0xabcd0003 || !strings.Contains(log, "probe-injection register si") {
		t.Fatal("未保留高位或注入紀錄")
	}
	if err := runScriptLine(p, "reg ip 0000 0001", quiet); err != nil || p.CPU.IP != 1 || p.CPU.Steps != 0 {
		t.Fatal("IP 注入不應執行指令", err)
	}
}

func TestWatchMemoryRecordsWriterAndFinalStep(t *testing.T) {
	// mov byte ptr ds:[0],7Bh；nop；hlt。
	p := probeProcess([]byte{0xc6, 0x06, 0x00, 0x00, 0x7b, 0x90, 0xf4})
	p.CPU.Seg[cpu.DS] = 23
	path := filepath.Join(t.TempDir(), "writes.jsonl")
	if err := watchMemoryUntil(p, []string{"000f:0005", "2", path, "0017:0000", "2"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 3 {
		t.Fatal(string(raw))
	}
	var row struct {
		Before probeState `json:"before_step"`
		After  probeState `json:"after_step"`
		Single bool       `json:"single_step"`
	}
	if err = json.Unmarshal([]byte(lines[1]), &row); err != nil {
		t.Fatal(err)
	}
	if row.Before.CSIP != "000F:0000" || row.After.CSIP != "000F:0005" || row.After.MemoryHex != "7b00" || !row.Single {
		t.Fatal(string(raw))
	}
	if !strings.Contains(lines[2], `"complete":true`) {
		t.Fatal(string(raw))
	}
	if err = watchMemoryUntil(p, []string{"000f:0000", "3", path, "0017:0000", "2"}); err == nil {
		t.Fatal("HLT 不可冒稱完成")
	}
	raw, _ = os.ReadFile(path)
	if !strings.Contains(string(raw), `"complete":false`) {
		t.Fatal(string(raw))
	}
}
