package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/wicanr2/wine-gorgon/internal/win16"
)

// 腳本是一串「跑幾步、點哪裡、按什麼鍵、存哪張圖」。
//
// 它刻意不是「等幾毫秒」：時間走的是 StepClock（跟指令數走），
// 所以「跑 N 條指令」才是可重現的單位。用真實時間會讓兩次執行不同。
//
//	run 200000        跑 200000 條指令（碰到錯誤就停）
//	click 300,200     在螢幕座標點一下
//	key 13            送一個虛擬鍵碼
//	type 你好         逐字送 WM_CHAR
//	shot out.png      把整個畫面存成 PNG
//	crop out.png x,y,w,h  只存畫面的一塊
//	raw out.bin       把索引原封不動寫出來
//	print             把目前的視窗列出來

func runScript(p *win16.Process, path string, echo func(string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if i := strings.Index(text, "#"); i >= 0 {
			text = strings.TrimSpace(text[:i])
		}
		if text == "" {
			continue
		}
		if err := runScriptLine(p, text, echo); err != nil {
			return fmt.Errorf("%s 第 %d 行（%s）：%w", path, line, text, err)
		}
	}
	return sc.Err()
}

func runScriptLine(p *win16.Process, text string, echo func(string)) error {
	fields := strings.Fields(text)
	cmd, args := fields[0], fields[1:]
	switch cmd {
	case "run":
		n, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return err
		}
		before := p.CPU.Steps
		pr := win16.NewProfile(997)
		runErr := p.RunProfiled(n, pr)
		echo(fmt.Sprintf("run %d → 實跑 %d 條，最熱的位置：", n, p.CPU.Steps-before))
		for _, line := range pr.Hot(5) {
			echo("    " + line)
		}
		return runErr
	case "waitfor":
		// 等到某個標題含這段文字的視窗出現。**用固定步數等是不可靠的**：
		// 同一個畫面在不同輸入下要跑的指令數差好幾倍，寫死就會在下一次
		// 改動之後靜靜地點到空氣。
		want := strings.Join(args, " ")
		limit := uint64(400_000_000)
		if i := strings.LastIndex(want, " @"); i >= 0 {
			if v, err := strconv.ParseUint(want[i+2:], 10, 64); err == nil {
				limit, want = v, strings.TrimSpace(want[:i])
			}
		}
		start := p.CPU.Steps
		for p.CPU.Steps-start < limit {
			if h, ok := findWindow(p, want); ok {
				echo(fmt.Sprintf("waitfor %q → 視窗 %04X（等了 %d 條指令）",
					want, h, p.CPU.Steps-start))
				return nil
			}
			if err := p.Run(2_000_000); err != nil {
				return err
			}
			if p.CPU.Halt {
				break
			}
		}
		return fmt.Errorf("等不到標題含 %q 的視窗（跑了 %d 條指令）", want, p.CPU.Steps-start)
	case "clickwin":
		// 點某個視窗的正中央。比寫死座標可靠：版面會隨字型度量改變。
		want := strings.Join(args, " ")
		h, ok := findWindow(p, want)
		if !ok {
			return fmt.Errorf("找不到標題含 %q 的視窗", want)
		}
		w, _ := p.Window(h)
		x, y := w.ClientX+w.ClientW/2, w.ClientY+w.ClientH/2
		got := p.Click(x, y)
		echo(fmt.Sprintf("clickwin %q → 視窗 %04X 於 (%d,%d)，命中 %04X", want, h, x, y, got))
		return nil
	case "click":
		x, y, err := pair(args[0])
		if err != nil {
			return err
		}
		h := p.Click(x, y)
		echo(fmt.Sprintf("click %d,%d → 視窗 %04X", x, y, h))
		return nil
	case "key":
		vk, err := strconv.ParseUint(args[0], 0, 16)
		if err != nil {
			return err
		}
		p.TypeKey(uint16(vk))
		return nil
	case "type":
		for _, r := range strings.Join(args, " ") {
			p.TypeKey(uint16(r))
		}
		return nil
	case "shot":
		return p.SavePNG(args[0], p.Screen)
	case "peek":
		// 印出執行時記憶體的內容。靜態 dump（nedump）看得到的是檔案映像，
		// 而「這個全域現在是多少」只有跑起來才知道——色組那一類 runtime
		// 才填的表就要靠它。
		var sel, off, n uint64
		parts := strings.SplitN(args[0], ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("peek 的位址要寫成 sel:off")
		}
		sel, err := strconv.ParseUint(parts[0], 16, 16)
		if err != nil {
			return err
		}
		off, err = strconv.ParseUint(parts[1], 16, 16)
		if err != nil {
			return err
		}
		n = 16
		if len(args) > 1 {
			if n, err = strconv.ParseUint(args[1], 0, 16); err != nil {
				return err
			}
		}
		var b []byte
		for i := uint64(0); i < n; i++ {
			v, err := p.Mod.Mem.ReadU8(uint16(sel), uint16(off+i))
			if err != nil {
				return err
			}
			b = append(b, v)
		}
		echo(fmt.Sprintf("peek %04X:%04X = % 02X", sel, off, b))
		return nil
	case "raw":
		return p.SaveIndexRaw(args[0], p.Screen)
	case "crop":
		x, y, w, h, err := quad(args[1])
		if err != nil {
			return err
		}
		return p.SavePNG(args[0], p.Screen.SubSurface(x, y, w, h))
	case "text":
		// 字還畫不出來，所以「畫面上有什麼字」只能從 TextOut 的紀錄看。
		n := 40
		if len(args) > 0 {
			if v, err := strconv.Atoi(args[0]); err == nil {
				n = v
			}
		}
		outs := p.TextOuts
		if len(outs) > n {
			outs = outs[len(outs)-n:]
		}
		for _, o := range outs {
			echo(fmt.Sprintf("  第 %d 步 視窗 %04X 螢幕 (%d,%d) %q",
				o.Steps, o.Window, o.ScreenX, o.ScreenY, o.Text))
		}
		return nil
	case "msgs":
		n := 40
		if len(args) > 0 {
			if v, err := strconv.Atoi(args[0]); err == nil {
				n = v
			}
		}
		// 第二個參數是「不要印的訊息編號」，用逗號分隔。訊息迴圈空轉時
		// WM_TIMER 會把紀錄淹掉，而卡住的原因不在它。
		skip := map[uint16]bool{}
		if len(args) > 1 {
			for _, f := range strings.Split(args[1], ",") {
				if v, err := strconv.ParseUint(f, 0, 16); err == nil {
					skip[uint16(v)] = true
				}
			}
		}
		var log []win16.MsgLogEntry
		for _, e := range p.MsgLog {
			if !skip[e.Message] {
				log = append(log, e)
			}
		}
		if len(log) > n {
			log = log[len(log)-n:]
		}
		echo(fmt.Sprintf("  訊息紀錄共 %d 則（上限 %d），濾掉後 %d 則",
			len(p.MsgLog), p.MsgLogSize, len(log)))
		for _, e := range log {
			name := win16.MsgName(e.Message)
			if name == "" {
				name = fmt.Sprintf("%04X", e.Message)
			}
			echo(fmt.Sprintf("  第 %d 步 %04X %-20s wParam=%04X lParam=%08X",
				e.Steps, e.HWnd, name, e.WParam, e.LParam))
		}
		return nil
	case "palette":
		for i := 0; i < 256; i++ {
			c := p.SysPalette[i]
			if c.R|c.G|c.B == 0 && i >= 10 && i < 246 {
				continue
			}
			echo(fmt.Sprintf("  pal %3d = %3d,%3d,%3d", i, c.R, c.G, c.B))
		}
		return nil
	case "hist":
		// 一塊區域裡各個調色盤索引出現幾次，附上它的 RGB。
		// 「顏色不對」要先分清楚是**索引錯**還是**索引對應的顏色錯**。
		x, y, w, h, err := quad(args[0])
		if err != nil {
			return err
		}
		counts := map[byte]int{}
		for j := 0; j < h; j++ {
			for i := 0; i < w; i++ {
				counts[p.Screen.At(x+i, y+j)]++
			}
		}
		type kv struct {
			i byte
			n int
		}
		var xs []kv
		for i, n := range counts {
			xs = append(xs, kv{i, n})
		}
		sort.Slice(xs, func(a, b int) bool { return xs[a].n > xs[b].n })
		for k, e := range xs {
			if k >= 12 {
				break
			}
			c := p.SysPalette[e.i]
			echo(fmt.Sprintf("  索引 %3d ×%-6d RGB(%3d,%3d,%3d)", e.i, e.n, c.R, c.G, c.B))
		}
		return nil
	case "print":
		for _, hw := range p.WindowOrder {
			w, ok := p.Window(hw)
			if !ok {
				continue
			}
			echo(fmt.Sprintf("  %04X %v (%d,%d %dx%d) 客戶 (%d,%d %dx%d) %q",
				w.Handle, w.Visible, w.X, w.Y, w.W, w.H,
				w.ClientX, w.ClientY, w.ClientW, w.ClientH, w.Text))
		}
		return nil
	}
	return fmt.Errorf("不認得的指令 %q", cmd)
}

// findWindow 找標題含指定文字的可見視窗（不分大小寫）。
func findWindow(p *win16.Process, want string) (uint16, bool) {
	if want == "" {
		return 0, false
	}
	lower := strings.ToLower(want)
	// **由上而下找**：`WindowOrder` 是建立順序，後建的在上面，而點擊
	// 落在最上層的那一個。由前往後找會挑到最舊的同名視窗——一連串
	// 對話框都有「OK」時，那就是點到已經看不見的那一個，然後整個
	// 腳本卡在後來才開的那個對話框的模態迴圈裡。
	for i := len(p.WindowOrder) - 1; i >= 0; i-- {
		h := p.WindowOrder[i]
		w, ok := p.Window(h)
		if !ok || !w.Visible {
			continue
		}
		if strings.Contains(strings.ToLower(w.Text), lower) {
			return h, true
		}
	}
	return 0, false
}

func pair(s string) (int, int, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("座標要寫成 x,y")
	}
	x, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	y, err := strconv.Atoi(parts[1])
	return x, y, err
}

func quad(s string) (int, int, int, int, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return 0, 0, 0, 0, fmt.Errorf("矩形要寫成 x,y,w,h")
	}
	var v [4]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		v[i] = n
	}
	return v[0], v[1], v[2], v[3], nil
}
