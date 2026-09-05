// nediff 逐點比對兩張 PNG 的某一塊，回報差異數並輸出差異圖。
//
// 這是對拍的量尺。它刻意只做一件事：**在指定的兩個矩形之間逐點比 RGB**。
// 不做縮放、不做容差、不做「近似相同」——「逐點相同」只有一個意思。
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"
	"strings"
)

func main() {
	aRect := flag.String("a-rect", "", "第一張圖的矩形 x,y,w,h（預設整張）")
	bRect := flag.String("b-rect", "", "第二張圖的矩形 x,y,w,h（預設整張）")
	out := flag.String("out", "", "把差異寫成 PNG（相同的像素變暗，不同的塗紅）")
	limit := flag.Int("list", 10, "列出前幾個不同的像素")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "用法：nediff [選項] <A.png> <B.png>")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), flag.Arg(1), *aRect, *bRect, *out, *limit); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(pathA, pathB, rectA, rectB, out string, listN int) error {
	a, err := load(pathA)
	if err != nil {
		return err
	}
	b, err := load(pathB)
	if err != nil {
		return err
	}
	ra, err := parseRect(rectA, a.Bounds())
	if err != nil {
		return fmt.Errorf("-a-rect：%w", err)
	}
	rb, err := parseRect(rectB, b.Bounds())
	if err != nil {
		return fmt.Errorf("-b-rect：%w", err)
	}
	if ra.Dx() != rb.Dx() || ra.Dy() != rb.Dy() {
		return fmt.Errorf("兩個矩形大小不同：%dx%d 對 %dx%d",
			ra.Dx(), ra.Dy(), rb.Dx(), rb.Dy())
	}

	w, h := ra.Dx(), ra.Dy()
	diff := image.NewRGBA(image.Rect(0, 0, w, h))
	bad := 0
	listed := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r1, g1, b1, _ := a.At(ra.Min.X+x, ra.Min.Y+y).RGBA()
			r2, g2, b2, _ := b.At(rb.Min.X+x, rb.Min.Y+y).RGBA()
			same := r1 == r2 && g1 == g2 && b1 == b2
			if same {
				diff.Set(x, y, color.RGBA{uint8(r1 >> 10), uint8(g1 >> 10), uint8(b1 >> 10), 255})
				continue
			}
			bad++
			diff.Set(x, y, color.RGBA{255, 0, 0, 255})
			if listed < listN {
				fmt.Printf("  (%d,%d) A=(%d,%d,%d) B=(%d,%d,%d)\n",
					x, y, r1>>8, g1>>8, b1>>8, r2>>8, g2>>8, b2>>8)
				listed++
			}
		}
	}
	total := w * h
	fmt.Printf("比對 %dx%d ＝ %d 個像素：不同 %d（%.4f%%）\n",
		w, h, total, bad, 100*float64(bad)/float64(total))
	if bad == 0 {
		fmt.Println("逐點相同")
	}
	if out != "" {
		f, err := os.Create(out)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := png.Encode(f, diff); err != nil {
			return err
		}
		fmt.Printf("差異圖寫到 %s\n", out)
	}
	if bad > 0 {
		os.Exit(1)
	}
	return nil
}

func load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func parseRect(s string, full image.Rectangle) (image.Rectangle, error) {
	if s == "" {
		return full, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return full, fmt.Errorf("矩形要寫成 x,y,w,h")
	}
	var v [4]int
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return full, err
		}
		v[i] = n
	}
	return image.Rect(v[0], v[1], v[0]+v[2], v[1]+v[3]), nil
}
