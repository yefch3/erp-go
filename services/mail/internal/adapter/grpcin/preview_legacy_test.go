package grpcin

import "testing"

// 修复前生成的任务缓存里，总价列存的是「=S2*T2」。重新生成会得到干净的新
// 版本，但已经存下的那些不该继续在页面上显示成坏的。
func TestLegacyPreviewHidesFormulaCells(t *testing.T) {
	rows := [][]string{
		{"热轧卷", "100", "", "=S2*T2"},
		{"冷轧卷", "50", "620.5", "=S3*T3"},
	}
	out := stripFormulaCells(rows)
	for i, row := range out {
		if row[3] != "" {
			t.Fatalf("第 %d 行的算式该被擦掉，实际 %q", i+1, row[3])
		}
		if row[0] == "" || row[1] == "" {
			t.Fatalf("第 %d 行不是算式的格子不该被动：%v", i+1, row)
		}
	}
	// 不能就地改：下载的文件仍然要那串算式。
	if rows[0][3] != "=S2*T2" {
		t.Fatalf("入参被就地改花了：%q", rows[0][3])
	}
}
