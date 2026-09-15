package app

import "testing"

// 改过的附件给最新那一版，没改过的给原件。「改过」要两样都齐：版本号和那一版
// 的键——只有版本号没有键（比如版本表里那一行的键是空的）不能算，那会把人
// 引向一个不存在的对象。
func TestLatestFilePicksTheEditedVersionOnlyWhenItReallyExists(t *testing.T) {
	cases := []struct {
		name       string
		revVersion int32
		revKey     string
		revSize    int64
		wantKey    string
		wantSize   int64
	}{
		{"没改过", 0, "", 0, "orig", 100},
		{"改过一版", 1, "rev1", 250, "rev1", 250},
		{"改过三版，给的是最新那一版", 3, "rev3", 80, "rev3", 80},
		{"有版本号没有键：当没改过", 2, "", 0, "orig", 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key, size := latestFile("orig", 100, c.revVersion, c.revKey, c.revSize)
			if key != c.wantKey || size != c.wantSize {
				t.Fatalf("latestFile = %q/%d, want %q/%d", key, size, c.wantKey, c.wantSize)
			}
		})
	}
}
