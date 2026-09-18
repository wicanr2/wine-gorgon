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

// TestWatchMemoryKeepGoingOnlyForExhaustion：`keepgoing` 只放寬步數用盡，
// 而且放寬之後收據仍然說得出自己被截斷了。CPU 結束不在放寬之列。
func TestWatchMemoryKeepGoingOnlyForExhaustion(t *testing.T) {
	tail := func(path string) map[string]any {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		var m map[string]any
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	// 無限迴圈：永遠命中不了目標，只會用盡步數。
	p := probeProcess([]byte{0xEB, 0xFE})
	path := filepath.Join(t.TempDir(), "writes.jsonl")
	if err := watchMemoryUntil(p, []string{"000f:00ff", "3", path, "0017:0000", "2"}); err == nil {
		t.Fatal("沒有 keepgoing 時，步數用盡要中止腳本")
	}
	p = probeProcess([]byte{0xEB, 0xFE})
	if err := watchMemoryUntil(p, []string{"000f:00ff", "3", path, "0017:0000", "2", "keepgoing"}); err != nil {
		t.Fatalf("keepgoing 應讓腳本繼續：%v", err)
	}
	m := tail(path)
	if m["complete"] != false || m["steps_exhausted"] != true || m["cpu_halted"] != false {
		t.Fatalf("被截斷的觀測不可看起來像完整的：%v", m)
	}
	if got, ok := m["not_observed"].([]any); !ok || len(got) < 3 {
		t.Fatalf("收據要列出看不到什麼（含被截斷這一項）：%v", m["not_observed"])
	}

	// HLT：程式真的跑完了，keepgoing 也不放行。
	p = probeProcess([]byte{0x90, 0xF4})
	if err := watchMemoryUntil(p, []string{"000f:00ff", "9", path, "0017:0000", "2", "keepgoing"}); err == nil {
		t.Fatal("CPU 結束不在 keepgoing 的放寬範圍")
	}
	if m := tail(path); m["cpu_halted"] != true || m["steps_exhausted"] != false {
		t.Fatalf("尾筆要分得出 HLT 與用盡步數：%v", m)
	}
}

// TestWatchMemoryMultipleRangesShareOneTrace：兩段範圍在同一條 trace 上，
// 每筆變更標得出是哪一段。分兩次跑得到的是兩條獨立觀測，不能拼接——所以要有這個。
func TestWatchMemoryMultipleRangesShareOneTrace(t *testing.T) {
	// mov byte ptr ds:[0],7Bh；mov byte ptr ds:[4],2Ah；nop；hlt。
	p := probeProcess([]byte{0xc6, 0x06, 0x00, 0x00, 0x7b, 0xc6, 0x06, 0x04, 0x00, 0x2a, 0x90, 0xf4})
	p.CPU.Seg[cpu.DS] = 23
	path := filepath.Join(t.TempDir(), "writes.jsonl")
	if err := watchMemoryUntil(p, []string{"000f:000a", "4", path,
		"0017:0000", "2", "0017:0004", "2"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	seen := map[float64]string{}
	for _, l := range lines[1 : len(lines)-1] {
		var row struct {
			Index float64    `json:"range_index"`
			Addr  string     `json:"range_address"`
			After probeState `json:"after_step"`
		}
		if err := json.Unmarshal([]byte(l), &row); err != nil {
			t.Fatal(err)
		}
		seen[row.Index] = row.After.MemoryHex
	}
	if seen[0] != "7b00" || seen[1] != "2a00" {
		t.Fatalf("兩段範圍都要各自記到自己的變更：%v\n%s", seen, raw)
	}
	var m struct {
		ByRange []int `json:"hits_by_range"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.ByRange) != 2 || m.ByRange[0] != 1 || m.ByRange[1] != 1 {
		t.Fatalf("尾筆要分段計數：%v", m.ByRange)
	}
}

// TestParseWatchAddressSegmentForm：段號寫法省掉呼叫端手算 (段號<<3)|7。
func TestParseWatchAddressSegmentForm(t *testing.T) {
	got, err := parseWatchAddress("seg92:3A0A")
	if err != nil {
		t.Fatal(err)
	}
	if got != (probeAddress{0x2E7, 0x3A0A}) {
		t.Fatalf("seg92:3A0A 應為 02E7:3A0A，得 %s", got)
	}
	if got, err := parseWatchAddress("042f:4ee0"); err != nil || got != (probeAddress{0x042F, 0x4EE0}) {
		t.Fatalf("原本的 selector 寫法要照舊：%v %v", got, err)
	}
	for _, bad := range []string{"seg:0000", "seg92", "segff:0000", "seg0:0000"} {
		if _, err := parseWatchAddress(bad); err == nil {
			t.Fatalf("%q 應被拒絕", bad)
		}
	}
}

// TestScriptContinuesAfterExhaustedWatch 是 A1 的驗收：第一段用盡 → 第二段照樣跑 →
// 兩份收據都在，而且都說得出自己是被截斷的。`until` 也吃 keepgoing。
func TestScriptContinuesAfterExhaustedWatch(t *testing.T) {
	dir := t.TempDir()
	p := probeProcess([]byte{0xEB, 0xFE}) // 無限迴圈：永遠命中不了目標
	var log []string
	echo := func(s string) { log = append(log, s) }
	first := filepath.Join(dir, "a.jsonl")
	second := filepath.Join(dir, "b.jsonl")
	for _, line := range []string{
		"watchmemuntil 000f:00ff 3 " + first + " seg2:0000 2 keepgoing",
		"until 000f:00ff 3 keepgoing",
		"watchmemuntil 000f:00ff 3 " + second + " 0017:0000 2 keepgoing",
	} {
		if err := runScriptLine(p, line, echo); err != nil {
			t.Fatalf("%s：%v", line, err)
		}
	}
	for _, path := range []string{first, second} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"complete":false`) ||
			!strings.Contains(string(raw), `"steps_exhausted":true`) {
			t.Fatalf("%s 的收據要標明被截斷：%s", path, raw)
		}
	}
	if len(log) == 0 || !strings.Contains(strings.Join(log, "\n"), "keepgoing") {
		t.Fatalf("繼續下去也要講一句，否則截斷的觀測看起來像完整的：%v", log)
	}
	// seg2:0000 要被換成 selector 0017：收據裡的位址就是證據。
	raw, _ := os.ReadFile(first)
	if !strings.Contains(string(raw), `"address":"0017:0000"`) {
		t.Fatalf("seg 寫法沒有換成 selector：%s", raw)
	}
}
