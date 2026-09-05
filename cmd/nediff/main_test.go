package main

import (
	"image"
	"testing"
)

func TestParseRect(t *testing.T) {
	full := image.Rect(0, 0, 100, 50)
	if got, err := parseRect("", full); err != nil || got != full {
		t.Errorf("空字串應該回整張：%v %v", got, err)
	}
	got, err := parseRect("10,20,30,40", full)
	if err != nil {
		t.Fatal(err)
	}
	if got != image.Rect(10, 20, 40, 60) {
		t.Errorf("解出 %v，預期 (10,20)-(40,60)", got)
	}
	if _, err := parseRect("1,2,3", full); err == nil {
		t.Error("少一個欄位應該被拒絕")
	}
}

// 忽略區是**指名的矩形**，不是放寬容差：右下界不含，和其他地方一致。
func TestIgnoreRectIsHalfOpen(t *testing.T) {
	rs := []image.Rectangle{image.Rect(10, 10, 12, 12)}
	for _, c := range []struct {
		x, y int
		want bool
	}{
		{10, 10, true}, {11, 11, true},
		{12, 11, false}, {11, 12, false}, {9, 10, false},
	} {
		if got := inAny(rs, c.x, c.y); got != c.want {
			t.Errorf("(%d,%d) 在忽略區內＝%v，預期 %v", c.x, c.y, got, c.want)
		}
	}
}
