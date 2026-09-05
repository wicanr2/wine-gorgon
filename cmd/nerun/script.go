package main

import (
	"bufio"
	"fmt"
	"os"
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
