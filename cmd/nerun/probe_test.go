package main

import (
	"bytes"
	"github.com/wicanr2/wine-gorgon/internal/cpu"
	"github.com/wicanr2/wine-gorgon/internal/win16"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func probeProcess(code []byte) *win16.Process {
	m := win16.NewMemory()
	m.Put(15, "合成程式", code)
	m.Put(23, "合成資料", make([]byte, 8))
	c := cpu.New(m)
	c.Seg[cpu.CS] = 15
	return &win16.Process{CPU: c, Mod: &win16.Module{Mem: m}}
}
func TestUntilExactBoundaryAndFailure(t *testing.T) {
	p := probeProcess([]byte{0x90, 0x90, 0xF4})
	if e := runUntil(p, probeAddress{15, 2}, 2, nil); e != nil {
		t.Fatal(e)
	}
	if p.CPU.Steps != 2 || p.CPU.Halt {
		t.Fatal("目標指令不應先執行")
	}
	if e := runUntil(p, probeAddress{15, 4}, 1, nil); e == nil {
		t.Fatal("CPU 結束不可假裝命中")
	}
	p = probeProcess([]byte{0xEB, 0xFE})
	if e := runUntil(p, probeAddress{15, 2}, 3, nil); e == nil || p.CPU.Steps != 3 {
		t.Fatal("無窮迴圈應按上限失敗")
	}
	if e := runUntil(p, probeAddress{15, 0}, 0, nil); e == nil {
		t.Fatal("零上限不可代表無界")
	}
}
func TestProbeWriteIsCompareAndSwap(t *testing.T) {
	p := probeProcess([]byte{0x90})
	quiet := func(string) {}
	for _, line := range []string{"poke", "until 000f:0000", "state", "traceuntil", "poke 0017:0000 01 ff", "poke 0017:ffff 0000 ffff", "poke 0017:0007 0000 ffff", "poke 0017:0000 00 ffff"} {
		if e := runScriptLine(p, line, quiet); e == nil {
			t.Fatalf("應拒絕 %s", line)
		}
		b, _ := p.Mod.Mem.Block(23)
		if !bytes.Equal(b.Data, make([]byte, 8)) {
			t.Fatal("失敗不得部分寫入")
		}
	}
	if e := runScriptLine(p, "poke 0017:0000 00000000 78563412", quiet); e != nil {
		t.Fatal(e)
	}
	b, _ := p.Mod.Mem.Block(23)
	if !bytes.Equal(b.Data[:4], []byte{0x78, 0x56, 0x34, 0x12}) {
		t.Fatal("種子原始位元組未保存")
	}
}
func TestTraceReplaysAndMarksTimeout(t *testing.T) {
	dir := t.TempDir()
	var first []byte
	for i := 0; i < 2; i++ {
		p := probeProcess([]byte{0x90, 0x90, 0xF4})
		path := filepath.Join(dir, "trace.jsonl")
		if _, e := probeCommand(p, "traceuntil", []string{"000f:0002", "2", "000f:0001", path, "0017:0000", "4"}, func(string) {}); e != nil {
			t.Fatal(e)
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(string(raw), `"complete":true`) || !strings.Contains(string(raw), `"hits":1`) {
			t.Fatal(string(raw))
		}
		if i == 0 {
			first = raw
		} else if !bytes.Equal(first, raw) {
			t.Fatal("相同輸入應產生相同追蹤")
		}
	}
	p := probeProcess([]byte{0xEB, 0xFE})
	path := filepath.Join(dir, "timeout.jsonl")
	if _, e := probeCommand(p, "traceuntil", []string{"000f:0002", "2", "000f:0000", path, "0017:0000", "4"}, func(string) {}); e == nil {
		t.Fatal("應逾限")
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"complete":false`) {
		t.Fatal("失敗追蹤不得冒充完整")
	}
}

func TestKeywinTargetsRequestedWindowAndPreservesFocus(t *testing.T) {
	p := probeProcess([]byte{0x90})
	p.Clock = &win16.FixedClock{}
	p.Windows = map[uint16]*win16.Window{20: {Handle: 20, Text: "CIVILIZATION", Visible: true}}
	p.WindowOrder = []uint16{20}
	p.Focus = 99
	if err := runScriptLine(p, "keywin 13 CIVILIZATION", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if p.Focus != 99 || len(p.Queue) != 3 {
		t.Fatal("不得改變焦點，應送出完整鍵序")
	}
	for _, m := range p.Queue {
		if m.HWnd != 20 || m.WParam != 13 {
			t.Fatal("送錯視窗")
		}
	}
	if err := runScriptLine(p, "keywin 13 missing", func(string) {}); err == nil {
		t.Fatal("不存在的視窗不得備援送到別處")
	}
}
