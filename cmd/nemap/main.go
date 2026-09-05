// nemap 把一個 NE 載入成位址空間，印出段配置、重定位統計與 thunk 表。
//
// 這是 M1 的對帳工具：重定位鏈走對了沒有，看「修補位置數」與「鏈數」的
// 比例就知道——NE 用鏈結串列表示同一個目標的多處修補，所以位置數會**遠大於**
// 重定位筆數。兩者相等代表鏈根本沒走。
//
//	go run ./cmd/nemap /path/to/GAME.EXE
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/wicanr2/wine-gorgon/internal/ne"
	"github.com/wicanr2/wine-gorgon/internal/win16"
)

func main() {
	showThunks := flag.Bool("thunks", false, "印出完整 thunk 表")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "用法：nemap [-thunks] <NE 執行檔>")
		os.Exit(2)
	}
	img, err := ne.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mod, err := win16.Load(img)
	if err != nil {
		fmt.Fprintln(os.Stderr, "載入失敗：", err)
		os.Exit(1)
	}

	relocs := 0
	for _, s := range img.Segments {
		relocs += len(s.Relocs)
	}
	total := 0
	for _, b := range mod.Mem.Blocks() {
		total += len(b.Data)
	}
	fmt.Printf("段 %d 塊，共 %d KiB；thunk %d 項\n",
		len(img.Segments), total/1024, len(mod.Thunks))
	fmt.Printf("重定位 %d 筆（加法式 %d、鏈結式 %d）→ 修補 %d 個位置，其中鏈上第二處以後 %d\n",
		relocs, relocs-mod.ChainsWalked, mod.ChainsWalked, mod.RelocsApplied, mod.ChainLinks)

	seg, off, err := img.Entry()
	if err == nil {
		fmt.Printf("進入點：selector %04X:%04X（段 %d）\n", win16.SegSelector(seg), off, seg)
	}

	if *showThunks {
		type row struct {
			off uint16
			imp ne.Import
		}
		rows := make([]row, 0, len(mod.Thunks))
		for i, imp := range mod.Thunks {
			rows = append(rows, row{uint16(i * win16.ThunkStride), imp})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].imp.Refs > rows[j].imp.Refs })
		fmt.Printf("\n=== thunk 表（%04X:位移 → API），依引用次數 ===\n", win16.ThunkSel)
		for _, r := range rows {
			fmt.Printf("  %04X:%04X  %5d  %s\n", win16.ThunkSel, r.off, r.imp.Refs, r.imp.Key())
		}
	}
}
