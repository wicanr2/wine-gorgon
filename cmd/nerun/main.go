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
	"sort"
	"strconv"
	"strings"

	"github.com/wicanr2/wine-gorgon/internal/cpu"
	"github.com/wicanr2/wine-gorgon/internal/ne"
	"github.com/wicanr2/wine-gorgon/internal/win16"
	"github.com/wicanr2/wine-gorgon/internal/winapi"
)

func main() {
	steps := flag.Uint64("steps", 200000, "最多執行幾條指令")
	trace := flag.Int("trace", 20, "結束時列出最後幾筆 API 呼叫")
	stub := flag.Bool("stub", false, "把每一支 API 都當成「回 0 就好」，用來看呼叫序列走多遠")
	data := flag.String("data", "", "原版資料目錄（唯讀掛成 C:\\CIV）")
	write := flag.String("write", "", "可寫目錄；不給就一律不准寫")
	shot := flag.String("shot", "", "結束時把整個畫面存成 PNG")
	screen := flag.String("screen", "640x480", "螢幕尺寸，例如 800x600")
	collapse := flag.Bool("collapse-palette", false, "把和靜態色相同的調色盤項收攏（原版量到的是不收攏）")
	openPath := flag.String("open", "", "檔案對話框要回傳的 DOS 路徑；空的表示使用者按取消")
	script := flag.String("script", "", "腳本檔：run／click／key／shot（見 cmd/nerun/script.go）")
	around := flag.String("around", "", "印出第一次呼叫這支 API 前後的紀錄（例：GDI.BITBLT）")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "用法：nerun [選項] <NE 檔>")
		os.Exit(2)
	}

	if err := run(flag.Arg(0), *steps, *trace, *stub, *data, *write, *shot, *script, *around, *screen, *openPath, *collapse); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string, steps uint64, traceN int, stub bool, data, write, shot, script, around, screen, openPath string, collapse bool) error {
	img, err := ne.Open(path)
	if err != nil {
		return err
	}
	mod, err := win16.Load(img)
	if err != nil {
		return err
	}
	sw, sh, err := parseSize(screen)
	if err != nil {
		return err
	}
	p, err := win16.NewProcessSized(mod, sw, sh)
	if err != nil {
		return err
	}
	p.FileDialogPath = openPath
	p.CollapsePalette = collapse

	win16.RegisterAll(p)
	if data != "" {
		p.FS.Root = data
	}
	p.FS.WriteRoot = write
	defer p.FS.CloseAll()
	if n, err := p.LoadInstalledFonts(); err == nil && n > 0 {
		fmt.Printf("載入字型 %d 個字面（%v）\n", n, p.FontFiles)
	}

	if stub {
		// 「其餘全部回 0」不是模擬，是**量測**：看沒接的 API 先當成成功的話，
		// 呼叫序列在崩掉之前能走多長。回傳值是假的，所以這條路徑上的
		// 任何結論都只能當線索，不能當證據。
		for _, imp := range mod.Thunks {
			key := imp.Key()
			if _, ok := p.Handlers[key]; ok {
				continue
			}
			if _, ok := p.RawHandlers[key]; ok {
				continue
			}
			if f, ok := winapi.Lookup(key); !ok || f.ArgBytes < 0 {
				continue
			}
			p.Handlers[key] = func(p *win16.Process, _ win16.Args) (uint32, error) { return 0, nil }
		}
	}

	c := p.CPU
	fmt.Printf("進入點 %04X:%04X，DS=%04X SS:SP=%04X:%04X\n",
		c.Seg[cpu.CS], c.IP, c.Seg[cpu.DS], c.Seg[cpu.SS], c.R16(cpu.SP))

	var runErr error
	if script != "" {
		runErr = runScript(p, script, func(s string) { fmt.Println(s) })
	} else {
		runErr = p.Run(steps)
	}
	fmt.Printf("執行 %d 條指令，攔到 %d 次 API 呼叫\n", c.Steps, p.Calls)

	if around != "" {
		for i, call := range p.Trace {
			if winapi.Describe(call.Import.Key()) != around {
				continue
			}
			lo := i - 15
			if lo < 0 {
				lo = 0
			}
			hi := i + 3
			if hi > len(p.Trace) {
				hi = len(p.Trace)
			}
			fmt.Printf("第一次 %s 前後：\n", around)
			for _, c2 := range p.Trace[lo:hi] {
				fmt.Printf("  #%-8d %-28s ← %04X:%04X\n", c2.Steps,
					winapi.Describe(c2.Import.Key()), c2.FromCS, c2.FromIP)
			}
			break
		}
	}
	recent := p.Trace
	if len(p.Recent) > 0 {
		recent = p.Recent
	}
	if n := len(recent); n > 0 && traceN > 0 {
		from := n - traceN
		if from < 0 {
			from = 0
		}
		fmt.Printf("最後 %d 筆呼叫：\n", n-from)
		for _, call := range recent[from:] {
			fmt.Printf("  #%-6d %-24s ← %04X:%04X\n", call.Steps, winapi.Describe(call.Import.Key()), call.FromCS, call.FromIP)
		}
	}

	for _, b := range p.MessageBoxes {
		fmt.Printf("訊息框（第 %d 步）[%s] %s\n", b.Steps, b.Caption, b.Text)
	}
	if len(p.FS.Opened) > 0 {
		fmt.Printf("開檔 %d 次：\n", len(p.FS.Opened))
		for _, r := range p.FS.Opened {
			status := "OK"
			if !r.OK {
				status = "找不到"
			}
			fmt.Printf("  %-24s %s\n", r.DOSPath, status)
		}
	}
	if len(p.Libraries) > 0 {
		fmt.Printf("LoadLibrary：%v\n", p.Libraries)
	}
	fmt.Printf("視窗 %d 個、GDI 物件 %d 個、blit %d 次、TextOut %d 次\n",
		len(p.Windows), p.Objects.Count(), p.Blits, len(p.TextOuts))
	if p.BlitsBadDC > 0 {
		fmt.Printf("BitBlt 因為 DC 不存在而畫不出來：%d 次\n", p.BlitsBadDC)
	}
	for _, hw := range p.WindowOrder {
		w, ok := p.Window(hw)
		if !ok {
			continue
		}
		kind := "視窗"
		if w.IsDialog {
			kind = "對話框"
		} else if w.ClassName != "" {
			kind = w.ClassName
		} else if w.Class != nil {
			kind = w.Class.Name
		}
		vis := " "
		if w.Visible {
			vis = "V"
		}
		fmt.Printf("  %04X %s %-10s (%d,%d %dx%d) 客戶 (%d,%d %dx%d) id=%d %q\n",
			w.Handle, vis, kind, w.X, w.Y, w.W, w.H,
			w.ClientX, w.ClientY, w.ClientW, w.ClientH, w.CtrlID, w.Text)
	}
	if shot != "" {
		if err := p.SavePNG(shot, p.Screen); err != nil {
			return err
		}
		fmt.Printf("畫面存到 %s（%d×%d）\n", shot, p.Screen.W, p.Screen.H)
	}
	{
		counts := map[string]int{}
		for k, n := range p.CallCount() {
			counts[winapi.Describe(k)] = n
		}
		type kv struct {
			k string
			n int
		}
		var xs []kv
		for k, n := range counts {
			xs = append(xs, kv{k, n})
		}
		sort.Slice(xs, func(i, j int) bool { return xs[i].n > xs[j].n })
		fmt.Println("呼叫次數：")
		for i, x := range xs {
			if i >= 60 {
				break
			}
			fmt.Printf("  %-32s %d\n", x.k, x.n)
		}
	}
	if len(p.Classes) > 0 {
		var names []string
		for n := range p.Classes {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Printf("註冊類別：%v\n", names)
	}
	if mc := p.MsgCount(); len(mc) > 0 {
		type kv struct {
			m uint16
			n int
		}
		var xs []kv
		for m, n := range mc {
			xs = append(xs, kv{m, n})
		}
		sort.Slice(xs, func(i, j int) bool { return xs[i].n > xs[j].n })
		fmt.Print("訊息：")
		for i, x := range xs {
			if i >= 10 {
				break
			}
			fmt.Printf("%04X×%d ", x.m, x.n)
		}
		fmt.Println()
	}
	{
		nonBlack := 0
		for i, e := range p.SysPalette {
			if i >= 10 && i < 246 && (e.R|e.G|e.B) != 0 {
				nonBlack++
			}
		}
		fmt.Printf("調色盤：邏輯→實體對應 %d 格，非靜態區有顏色的 %d／236\n",
			len(p.PalMap), nonBlack)
	}
	if len(p.BitmapKinds) > 0 {
		fmt.Print("CreateBitmap 的 (平面,bpp)：")
		for k, n := range p.BitmapKinds {
			fmt.Printf("(%d,%d)×%d ", k[0], k[1], n)
		}
		fmt.Println()
	}
	if len(p.Sounds) > 0 {
		fmt.Printf("音效：%v\n", p.Sounds)
	}
	for _, n := range p.Notes {
		fmt.Printf("備註：%s\n", n)
	}

	if runErr == nil {
		fmt.Println("停在：步數上限或 HLT")
		return nil
	}

	var api *win16.UnhandledAPIError
	var ce *cpu.Error
	switch {
	case errors.As(runErr, &api):
		fmt.Printf("停在未實作的 API：%s\n", winapi.Describe(api.Import.Key()))
	case errors.As(runErr, &ce):
		fmt.Printf("停在 CPU：%s\n", ce.Error())
	default:
		fmt.Printf("停在：%v\n", runErr)
	}
	fmt.Printf("暫存器 AX=%04X BX=%04X CX=%04X DX=%04X SI=%04X DI=%04X BP=%04X SP=%04X\n",
		c.R16(cpu.AX), c.R16(cpu.BX), c.R16(cpu.CX), c.R16(cpu.DX), c.R16(cpu.SI), c.R16(cpu.DI), c.R16(cpu.BP), c.R16(cpu.SP))
	fmt.Printf("         CS=%04X IP=%04X DS=%04X ES=%04X SS=%04X FLAGS=%04X\n",
		c.Seg[cpu.CS], c.IP, c.Seg[cpu.DS], c.Seg[cpu.ES], c.Seg[cpu.SS], c.Flags)
	return nil
}

func parseSize(s string) (int, int, error) {
	i := strings.IndexAny(s, "xX*")
	if i < 0 {
		return 0, 0, fmt.Errorf("螢幕尺寸要寫成 寬x高，例如 800x600")
	}
	w, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, 0, err
	}
	h, err := strconv.Atoi(s[i+1:])
	return w, h, err
}
