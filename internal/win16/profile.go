package win16

import (
	"fmt"
	"sort"
)

// Profile 是「程式現在在哪裡」的抽樣統計。
//
// 沒有畫面、沒有 API 呼叫、指令卻一直在跑，那就是卡在某個迴圈裡。
// 這種時候唯一有用的資訊是 CS:IP 的分布——而且要**抽樣**，
// 逐條記錄會慢到不能用。
type Profile struct {
	Every   uint64 // 每幾條指令抽一次
	samples map[uint32]int
	total   int
}

// NewProfile 造一個抽樣器。
func NewProfile(every uint64) *Profile {
	if every == 0 {
		every = 1000
	}
	return &Profile{Every: every, samples: map[uint32]int{}}
}

// Sample 記一筆（由 Process 的執行迴圈呼叫）。
func (pr *Profile) Sample(cs, ip uint16) {
	pr.samples[uint32(cs)<<16|uint32(ip)]++
	pr.total++
}

// Hot 回傳最熱的 n 個位置。
func (pr *Profile) Hot(n int) []string {
	type kv struct {
		at uint32
		n  int
	}
	var xs []kv
	for at, c := range pr.samples {
		xs = append(xs, kv{at, c})
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i].n > xs[j].n })
	var out []string
	for i, x := range xs {
		if i >= n {
			break
		}
		out = append(out, fmt.Sprintf("%04X:%04X %5.1f%%（%d 次）",
			x.at>>16, x.at&0xFFFF, 100*float64(x.n)/float64(pr.total), x.n))
	}
	return out
}

// RunProfiled 跑指定步數，順便抽樣。
func (p *Process) RunProfiled(maxSteps uint64, pr *Profile) error {
	for n := uint64(0); n < maxSteps; n++ {
		if p.CPU.Halt {
			return nil
		}
		if pr != nil && p.CPU.Steps%pr.Every == 0 {
			pr.Sample(p.CPU.Seg[1], p.CPU.IP) // 1 ＝ cpu.CS
		}
		if err := p.CPU.Step(); err != nil {
			return err
		}
	}
	return nil
}
