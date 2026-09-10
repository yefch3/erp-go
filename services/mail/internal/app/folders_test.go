package app

import (
	"errors"
	"testing"
)

// 只认明确说「默认/系统文件夹」的答复。泛泛的 "can't rename" 不算：那可能是
// 名字撞了，误判会把一个正当的自建文件夹永久锁成系统。
func TestHostSaysDefaultFolderOnlyOnExplicitWording(t *testing.T) {
	yes := []string{
		"RENAME can't rename default folder or Invalid folder name", // 263
		"[CANNOT] System folder cannot be renamed",                  // Gmail
		"DELETE System folder cannot be deleted",
	}
	no := []string{
		"Can't rename mailbox to 客户: Mailbox already exists", // Dovecot：名字撞了
		"can't delete: connection reset by peer",
		"NO [TRYCREATE] Mailbox doesn't exist",
		"",
	}
	for _, m := range yes {
		if !hostSaysDefaultFolder(errors.New(m)) {
			t.Errorf("应该认出是默认文件夹：%q", m)
		}
	}
	for _, m := range no {
		if hostSaysDefaultFolder(errors.New(m)) {
			t.Errorf("不该当成默认文件夹：%q", m)
		}
	}
	if hostSaysDefaultFolder(nil) {
		t.Error("nil 不是拒绝")
	}
}
