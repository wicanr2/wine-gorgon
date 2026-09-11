package win16

import (
	"os"
	"path/filepath"
	"testing"
)

// 寫一份最小的 cue 與對應大小的映像，驗長度算得對。
func writeDisc(t *testing.T, cue string, frames int) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "d.bin")
	if err := os.WriteFile(bin, make([]byte, frames*2352), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "d.cue")
	if err := os.WriteFile(path, []byte(cue), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCueLengths(t *testing.T) {
	// 三軌：資料軌 0..149、音軌 150..224（1 秒）、音軌 225..374（2 秒）。
	cue := `FILE "d.bin" BINARY
  TRACK 01 MODE1/2352
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    INDEX 01 00:02:00
  TRACK 03 AUDIO
    INDEX 01 00:03:00
`
	d, err := LoadCue(writeDisc(t, cue, 375))
	if err != nil {
		t.Fatal(err)
	}
	if d.Count() != 3 {
		t.Fatalf("音軌數 = %d，要 3", d.Count())
	}
	if got := d.TrackFrames(2); got != 75 {
		t.Errorf("第 2 軌 %d 幀，要 75", got)
	}
	if got := d.TrackFrames(3); got != 150 {
		t.Errorf("第 3 軌 %d 幀，要 150", got)
	}
	if got := d.TrackStart(3); got != 225 {
		t.Errorf("第 3 軌起點 %d，要 225", got)
	}
	// 不存在的編號回 0，不是 panic 也不是最後一軌。
	if got := d.TrackFrames(9); got != 0 {
		t.Errorf("不存在的音軌回 %d，要 0", got)
	}
}

func TestLoadCueRejectsMultiFile(t *testing.T) {
	cue := "FILE \"a.bin\" BINARY\n  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n" +
		"FILE \"b.bin\" BINARY\n  TRACK 02 AUDIO\n    INDEX 01 00:00:00\n"
	if _, err := LoadCue(writeDisc(t, cue, 10)); err == nil {
		t.Fatal("多檔 cue 應該回錯誤，不是猜一個 TOC")
	}
}

func TestFramesToMSF(t *testing.T) {
	// 3 分 31 秒 20 幀。
	got := FramesToMSF((3*60+31)*75 + 20)
	if want := uint32(3 | 31<<8 | 20<<16); got != want {
		t.Errorf("MSF = %06X，要 %06X", got, want)
	}
}

// TestPTO2DiscFingerprint 拿原版 CD 的 cue 對 EXE 裡寫死的指紋表。
//
// 這是 CD 檢查那條路的正對照：表在 TEKE2WIN.EXE 的 DGROUP:0x432A，14 個 word，
// 音軌 2..15 每條的秒數要落在 ±5 內（RE:cseg06:0x22de）。缺檔就 skip——
// 安靜的替代品會讓「還沒做完」看起來像做完了。
func TestPTO2DiscFingerprint(t *testing.T) {
	cue := os.Getenv("PTO2_CUE")
	if cue == "" {
		t.Skip("沒有設 PTO2_CUE，跳過")
	}
	d, err := LoadCue(cue)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{211, 198, 179, 182, 212, 205, 208, 237, 223, 133, 200, 199, 228, 145}
	for i, w := range want {
		track := i + 2
		got := d.TrackFrames(track) / FramesPerSecond
		if got < w-5 || got > w+5 {
			t.Errorf("第 %d 軌 %d 秒，指紋表要 %d±5", track, got, w)
		}
	}
}
