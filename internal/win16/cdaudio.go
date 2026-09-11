package win16

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Disc 是一張光碟的目錄（TOC）：每條音軌從哪一幀開始、整張到哪裡結束。
//
// 為什麼要真的讀 TOC：老遊戲常拿「音軌長度」當光碟指紋。PTO2 就是——
// 它對音軌 2..15 逐條量長度，和 EXE 裡寫死的 14 個秒數比對（±5 秒）。
// 回一個看起來合理的假值過不了關，而且也不該過：oracle 的工作是誠實
// 回答「玩家手上這張是什麼」，玩家本來就有那張 CD。
type Disc struct {
	Tracks []DiscTrack
	// End 是最後一幀之後的 LBA，也就是整張的長度（幀）。
	End int
}

// DiscTrack 是一條音軌。CD 的時間單位是幀，一秒 75 幀。
type DiscTrack struct {
	Num   int
	Audio bool
	Start int // LBA
}

// FramesPerSecond 是 CD 的時間基準。
const FramesPerSecond = 75

// LoadCue 讀一份 .cue，連同同目錄的映像檔算出整張的長度。
//
// 只支援單一 FILE 的 cue（rip 出來的 bin/cue 幾乎都是這種）。多檔 cue
// 會回錯誤而不是猜——猜錯的 TOC 會讓指紋比對安靜地失敗。
func LoadCue(path string) (*Disc, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var (
		d         Disc
		binName   string
		files     int
		sectorLen int
		cur       *DiscTrack
	)
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "FILE":
			files++
			if files > 1 {
				return nil, fmt.Errorf("%s：多檔 cue 尚未支援", filepath.Base(path))
			}
			if i := strings.IndexByte(sc.Text(), '"'); i >= 0 {
				if j := strings.IndexByte(sc.Text()[i+1:], '"'); j >= 0 {
					binName = sc.Text()[i+1 : i+1+j]
				}
			}
		case "TRACK":
			if len(fields) < 3 {
				return nil, fmt.Errorf("%s 第 %d 行：TRACK 缺欄位", filepath.Base(path), line)
			}
			n, err := strconv.Atoi(fields[1])
			if err != nil {
				return nil, fmt.Errorf("%s 第 %d 行：音軌編號 %q 不是數字", filepath.Base(path), line, fields[1])
			}
			mode := strings.ToUpper(fields[2])
			d.Tracks = append(d.Tracks, DiscTrack{Num: n, Audio: mode == "AUDIO"})
			cur = &d.Tracks[len(d.Tracks)-1]
			// 整張映像的每扇區位元組數由資料軌的宣告決定；AUDIO 恆為 2352。
			switch {
			case mode == "AUDIO" || strings.HasSuffix(mode, "/2352"):
				if sectorLen == 0 {
					sectorLen = 2352
				}
			case strings.HasSuffix(mode, "/2048"):
				sectorLen = 2048
			default:
				return nil, fmt.Errorf("%s 第 %d 行：不認得的音軌型別 %q", filepath.Base(path), line, mode)
			}
		case "INDEX":
			// 只取 INDEX 01（曲子真正開始的地方）；INDEX 00 是前導間隔。
			if cur == nil || len(fields) < 3 || fields[1] != "01" {
				continue
			}
			lba, err := parseMSF(fields[2])
			if err != nil {
				return nil, fmt.Errorf("%s 第 %d 行：%w", filepath.Base(path), line, err)
			}
			cur.Start = lba
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(d.Tracks) == 0 {
		return nil, fmt.Errorf("%s：沒有任何音軌", filepath.Base(path))
	}
	if binName == "" {
		return nil, fmt.Errorf("%s：找不到 FILE 這一行", filepath.Base(path))
	}
	st, err := os.Stat(filepath.Join(filepath.Dir(path), binName))
	if err != nil {
		return nil, err
	}
	if sectorLen == 0 {
		sectorLen = 2352
	}
	d.End = int(st.Size() / int64(sectorLen))
	return &d, nil
}

// TrackFrames 回傳某條音軌的長度（幀）。編號不存在時回 0。
func (d *Disc) TrackFrames(num int) int {
	for i, t := range d.Tracks {
		if t.Num != num {
			continue
		}
		end := d.End
		if i+1 < len(d.Tracks) {
			end = d.Tracks[i+1].Start
		}
		if end < t.Start {
			return 0
		}
		return end - t.Start
	}
	return 0
}

// TrackStart 回傳某條音軌的起點（幀）。編號不存在時回 0。
func (d *Disc) TrackStart(num int) int {
	for _, t := range d.Tracks {
		if t.Num == num {
			return t.Start
		}
	}
	return 0
}

// Count 是音軌總數。
func (d *Disc) Count() int { return len(d.Tracks) }

// parseMSF 把 cue 的 `mm:ss:ff` 換成 LBA。
func parseMSF(s string) (int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("時間 %q 要寫成 mm:ss:ff", s)
	}
	var v [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, fmt.Errorf("時間 %q 不是數字", s)
		}
		v[i] = n
	}
	return (v[0]*60+v[1])*FramesPerSecond + v[2], nil
}

// FramesToMSF 把幀數打包成 MCI 的 MSF：byte0 分、byte1 秒、byte2 幀。
func FramesToMSF(frames int) uint32 {
	if frames < 0 {
		frames = 0
	}
	f := frames % FramesPerSecond
	total := frames / FramesPerSecond
	return uint32(total/60&0xFF) | uint32(total%60&0xFF)<<8 | uint32(f&0xFF)<<16
}
