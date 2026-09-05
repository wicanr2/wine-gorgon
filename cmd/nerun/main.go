// nerun 把一個 NE 載進位址空間、從進入點開始單步跑，跑到第一個未實作的
// API（或第一個未實作的 opcode）為止，然後把停在哪裡講清楚。
//
// 這支的產出就是下一步的工作清單：訊息裡的 API 名稱或 opcode 位址，
// 直接對應到「接下來要實作什麼」。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/wicanr2/wine-gorgon/internal/cpu"
	"github.com/wicanr2/wine-gorgon/internal/ne"
	"github.com/wicanr2/wine-gorgon/internal/win16"
)

func main() {
	steps := flag.Uint64("steps", 200000, "最多執行幾條指令")
	trace := flag.Int("trace", 20, "結束時列出最後幾筆 API 呼叫")
	stub := flag.Bool("stub", false, "把每一支 API 都當成「回 0 就好」，用來看呼叫序列走多遠")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "用法：nerun [選項] <NE 檔>")
		os.Exit(2)
	}

	if err := run(flag.Arg(0), *steps, *trace, *stub); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string, steps uint64, traceN int, stub bool) error {
	img, err := ne.Open(path)
	if err != nil {
		return err
	}
	mod, err := win16.Load(img)
	if err != nil {
		return err
	}
	p, err := win16.NewProcess(mod)
	if err != nil {
		return err
	}

	if stub {
		// 「全部回 0」不是模擬，是**量測**：看不接任何 API 的話，
		// 呼叫序列在崩掉之前能走多長。參數位元組數不知道，所以不彈參數，
		// 堆疊會漏——這只是探路，不是可信的執行。
		for _, imp := range mod.Thunks {
			p.Handlers[imp.Key()] = func(p *win16.Process, _ ne.Import) error {
				p.CPU.R[cpu.AX] = 0
				p.CPU.R[cpu.DX] = 0
				return p.CPU.RetFar(0)
			}
		}
	}

	c := p.CPU
	fmt.Printf("進入點 %04X:%04X，DS=%04X SS:SP=%04X:%04X\n",
		c.Seg[cpu.CS], c.IP, c.Seg[cpu.DS], c.Seg[cpu.SS], c.R[cpu.SP])

	runErr := p.Run(steps)
	fmt.Printf("執行 %d 條指令，攔到 %d 次 API 呼叫\n", c.Steps, len(p.Trace))

	if n := len(p.Trace); n > 0 && traceN > 0 {
		from := n - traceN
		if from < 0 {
			from = 0
		}
		fmt.Printf("最後 %d 筆呼叫：\n", n-from)
		for _, call := range p.Trace[from:] {
			fmt.Printf("  #%-6d %-24s ← %04X:%04X\n", call.Steps, call.Import.Key(), call.FromCS, call.FromIP)
		}
	}

	if runErr == nil {
		fmt.Println("停在：步數上限或 HLT")
		return nil
	}

	var api *win16.UnhandledAPIError
	var ce *cpu.Error
	switch {
	case errors.As(runErr, &api):
		fmt.Printf("停在未實作的 API：%s\n", api.Import.Key())
	case errors.As(runErr, &ce):
		fmt.Printf("停在 CPU：%s\n", ce.Error())
	default:
		fmt.Printf("停在：%v\n", runErr)
	}
	fmt.Printf("暫存器 AX=%04X BX=%04X CX=%04X DX=%04X SI=%04X DI=%04X BP=%04X SP=%04X\n",
		c.R[cpu.AX], c.R[cpu.BX], c.R[cpu.CX], c.R[cpu.DX], c.R[cpu.SI], c.R[cpu.DI], c.R[cpu.BP], c.R[cpu.SP])
	fmt.Printf("         CS=%04X IP=%04X DS=%04X ES=%04X SS=%04X FLAGS=%04X\n",
		c.Seg[cpu.CS], c.IP, c.Seg[cpu.DS], c.Seg[cpu.ES], c.Seg[cpu.SS], c.Flags)
	return nil
}
