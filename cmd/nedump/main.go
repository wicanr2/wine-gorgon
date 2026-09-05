// nedump 印出載入後某個段的位元組，並標出落在範圍內的重定位。
//
// 用途是「nerun 停在 000F:41AF，那裡到底寫了什麼」——把執行時看到的
// selector:位移直接餵進來就好，不必自己換算檔案位移。
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wicanr2/wine-gorgon/internal/ne"
	"github.com/wicanr2/wine-gorgon/internal/win16"
	"github.com/wicanr2/wine-gorgon/internal/winapi"
)

func main() {
	at := flag.String("at", "", "位置，格式 selector:位移（例：F00F:0000 或 000F:41AF）")
	n := flag.Int("len", 64, "印幾個位元組")
	flag.Parse()
	if flag.NArg() != 1 || *at == "" {
		fmt.Fprintln(os.Stderr, "用法：nedump -at <sel:off> [-len N] <NE 檔>")
		os.Exit(2)
	}
	sel, off, err := parseAt(*at)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	img, err := ne.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mod, err := win16.Load(img)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for i := 0; i < *n; i += 16 {
		fmt.Printf("%04X:%04X ", sel, off+uint16(i))
		var text strings.Builder
		for j := 0; j < 16 && i+j < *n; j++ {
			b, err := mod.Mem.ReadU8(sel, off+uint16(i+j))
			if err != nil {
				fmt.Printf("-- ")
				continue
			}
			fmt.Printf("%02X ", b)
			if b >= 0x20 && b < 0x7F {
				text.WriteByte(b)
			} else {
				text.WriteByte('.')
			}
		}
		fmt.Printf(" %s\n", text.String())
	}

	// 範圍內的重定位：這才是「這個 far call 打到誰」的答案。
	seg := win16.SelSegment(sel)
	for _, s := range img.Segments {
		if s.Index != seg {
			continue
		}
		for _, r := range s.Relocs {
			if int(r.Offset) < int(off) || int(r.Offset) >= int(off)+*n {
				continue
			}
			desc := ""
			if imp, err := img.ImportForReloc(r); err == nil {
				desc = winapi.Describe(imp.Key())
			} else {
				desc = fmt.Sprintf("段 %d:%04X", r.TargetSeg, r.TargetOff)
			}
			fmt.Printf("  重定位 +%04X → %s\n", r.Offset, desc)
		}
	}
}

func parseAt(s string) (uint16, uint16, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("位置要寫成 selector:位移")
	}
	sel, err := strconv.ParseUint(parts[0], 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("selector 不是十六進位：%s", parts[0])
	}
	off, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("位移不是十六進位：%s", parts[1])
	}
	return uint16(sel), uint16(off), nil
}
