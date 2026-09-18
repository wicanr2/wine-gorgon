package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wicanr2/wine-gorgon/internal/win16"
)

// 位址的兩種寫法。
//
// `sel:off` 是執行器實際用的 selector（十六進位）。`seg<十進位段號>:off` 是
// NE 段號寫法，由 `(段號 << 3) | 7` 換算（`docs/spec/002-address-space.md` §1）。
//
// 為什麼要第二種：呼叫端手算 selector **算錯不會報錯**——那個位址多半仍然可讀，
// 只是讀到別的東西，於是整份收據看起來完整而內容是錯的。換算收進工具裡就少一類靜默錯誤。
//
// 沒有 `lin:`（IDA 線性位址）：那個位址空間是反組譯器攤平出來的，執行器手上沒有
// 對照表，硬做只會變成第二份會漂的資料。要用線性位址的呼叫端自己在遊戲專案裡換算。
func parseWatchAddress(s string) (probeAddress, error) {
	rest, ok := strings.CutPrefix(strings.ToLower(s), "seg")
	if !ok {
		return parseProbeAddress(s)
	}
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return probeAddress{}, fmt.Errorf("段號位址必須為 seg<段號>:offset：%q", s)
	}
	seg, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return probeAddress{}, fmt.Errorf("段號要寫十進位：%q", s)
	}
	if seg == 0 || seg > 0x1FFF {
		return probeAddress{}, fmt.Errorf("段號 %d 超出範圍", seg)
	}
	off, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return probeAddress{}, err
	}
	return probeAddress{uint16(seg<<3 | 7), uint16(off)}, nil
}

// cutKeepGoing 把結尾的 `keepgoing` 切出來。
//
// 它只放寬**步數用盡**這一種結果：那是「這段期間看完了」，不是失敗，而收據尾筆
// 本來就記著 `complete:false`。CPU 結束（HLT）、參數錯誤、寫檔失敗仍然中止腳本——
// 那些是真的壞掉了。
func cutKeepGoing(args []string) ([]string, bool) {
	if n := len(args); n > 0 && strings.EqualFold(args[n-1], "keepgoing") {
		return args[:n-1], true
	}
	return args, false
}

// watchTarget 是一段被監看的記憶體。
//
// 多段的意義是**同一條 trace**：分兩次跑得到的是兩條獨立觀測，先後關係不能拼接
// （這條紀律來自下游 civ1 專案：獨立執行的觀測不得接成同一條時間軸）。
type watchTarget struct {
	index int
	addr  probeAddress
	n     uint64
	prev  probeState
}

// parseWatchTargets 讀「一或多組 memory length」。
func parseWatchTargets(args []string) ([]*watchTarget, error) {
	if len(args)%2 != 0 || len(args) == 0 {
		return nil, fmt.Errorf("記憶體範圍要成對給（memory length）")
	}
	var out []*watchTarget
	for i := 0; i < len(args); i += 2 {
		addr, err := parseWatchAddress(args[i])
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseUint(args[i+1], 0, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, &watchTarget{index: len(out), addr: addr, n: n})
	}
	return out, nil
}

// notObserved 是「這份收據看不到什麼」，直接寫進尾筆。
//
// 收據只說得出自己看到的東西；**看不到的那些，讀的人分不出是「沒發生」還是
// 「沒看到」**，所以由工具自己列出來，不要留給下游猜。
func notObserved(exhausted bool) []string {
	out := []string{
		"同值寫入：逐外層步驟比對值，寫回相同值不產生紀錄",
		"巢狀 Win16 回呼裡的寫入：只看得到回呼結束後的合併結果（single_step=false）",
	}
	if exhausted {
		out = append(out, "用盡步數之後的一切：這段觀測在時間上是被截斷的")
	}
	return out
}

// normalizeSegmentAddresses 把命令參數裡的 `seg<段號>:off` 就地換成 `sel:off`。
//
// 放在總入口而不是每支命令裡：`until`／`poke`／`state`／`traceuntil` 都吃位址，
// 逐支去改會漏掉一支，而漏掉的那支不會報錯——只會安靜地看別的地方。
func normalizeSegmentAddresses(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, a := range out {
		if !strings.HasPrefix(strings.ToLower(a), "seg") {
			continue
		}
		if addr, err := parseWatchAddress(a); err == nil {
			out[i] = addr.String()
		}
	}
	return out
}

// probeKeepGoing 讓 `until`／`traceuntil` 也能帶 `keepgoing`。
//
// `watchmemuntil` 自己處理（它要在收據尾筆標 `steps_exhausted`），所以不走這裡。
// 判斷「步數用盡」用的是 CPU 的事實（沒有 HLT、而且真的跑掉了上限那麼多步），
// 不是比對錯誤訊息的字串——訊息會改，事實不會。
func probeKeepGoing(p *win16.Process, cmd string, args []string, echo func(string)) (bool, error) {
	if cmd != "until" && cmd != "traceuntil" {
		return false, nil
	}
	rest, keep := cutKeepGoing(args)
	if !keep {
		return false, nil
	}
	if len(rest) < 2 {
		return true, fmt.Errorf("%s 需要 target 與步數上限", cmd)
	}
	limit, err := strconv.ParseUint(rest[1], 0, 64)
	if err != nil {
		return true, err
	}
	start := p.CPU.Steps
	handled, runErr := probeCommand(p, cmd, rest, echo)
	if !handled {
		return true, fmt.Errorf("未知的命令 %q", cmd)
	}
	if runErr == nil {
		return true, nil
	}
	if p.CPU.Halt || p.CPU.Steps-start < limit {
		return true, runErr
	}
	echo(fmt.Sprintf("%s：%v；keepgoing → 繼續下一行", cmd, runErr))
	return true, nil
}
