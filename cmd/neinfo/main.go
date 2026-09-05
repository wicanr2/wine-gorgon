// neinfo 印出一個 Win16 NE 執行檔的段、匯入與進入點。
//
// 這是 wine-gorgon 的第一個工具，也是第一個**可對帳**的產物：
// 匯入清單就是「這個程式需要多少 Win16 API」，也就是模擬層要實作的表面。
// 先量再寫，不要憑印象決定範圍。
//
//	go run ./cmd/neinfo /path/to/GAME.EXE
//	go run ./cmd/neinfo -imports /path/to/GAME.EXE   # 只印匯入，依引用次數排序
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/wicanr2/wine-gorgon/internal/ne"
)

func main() {
	onlyImports := flag.Bool("imports", false, "只印匯入清單")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "用法：neinfo [-imports] <NE 執行檔>")
		os.Exit(2)
	}
	img, err := ne.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	byMod := img.ImportsByModule()
	mods := make([]string, 0, len(byMod))
	for m := range byMod {
		mods = append(mods, m)
	}
	sort.Strings(mods)

	if !*onlyImports {
		seg, off, err := img.Entry()
		entry := "（無）"
		if err == nil {
			entry = fmt.Sprintf("段 %d:%04X", seg, off)
		}
		fmt.Printf("NE 檔頭 0x%X；段 %d；模組參考 %d；進入點 %s\n",
			img.HeaderOff, len(img.Segments), len(img.ModuleNames), entry)
		fmt.Printf("堆疊 %d；heap %d；sector shift %d\n\n",
			img.StackSize, img.HeapSize, img.SectorShift)

		code, data, movable, relocs := 0, 0, 0, 0
		for _, s := range img.Segments {
			if s.IsData() {
				data++
			} else {
				code++
			}
			if s.Movable() {
				movable++
			}
			relocs += len(s.Relocs)
		}
		fmt.Printf("程式段 %d、資料段 %d、可移動 %d；重定位 %d 筆\n\n",
			code, data, movable, relocs)
	}

	total := 0
	fmt.Println("=== 匯入表面（每個模組的相異項數）===")
	for _, m := range mods {
		fmt.Printf("  %-10s %4d\n", m, len(byMod[m]))
		total += len(byMod[m])
	}
	refs := 0
	for _, imp := range img.Imports {
		refs += imp.Refs
	}
	fmt.Printf("\n相異匯入 %d 項，被 %d 筆重定位引用\n", total, refs)

	if *onlyImports {
		sorted := append([]ne.Import(nil), img.Imports...)
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Refs != sorted[j].Refs {
				return sorted[i].Refs > sorted[j].Refs
			}
			return sorted[i].Key() < sorted[j].Key()
		})
		fmt.Println("\n=== 依引用次數 ===")
		for _, imp := range sorted {
			fmt.Printf("  %5d  %s\n", imp.Refs, imp.Key())
		}
	}
}
