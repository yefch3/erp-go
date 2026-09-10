package app

import (
	"testing"
	"time"
)

// 落库的那份结论，读回来要接着上次走。
//
// 这几条钉的是「重启之后不用重新被掐三圈」，以及它的反面——冷静期不能因为
// 重启而被无限延长，否则一台早就改好的服务器永远回不到推送。
func TestSeedFromStoredMode(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	t.Run("上次判定轮询，冷静期还没过：接着安静", func(t *testing.T) {
		var h idleHealth
		h.seed(PushModePoll, now.Add(-10*time.Minute), now)
		if h.idleWorthTrying(now) {
			t.Fatal("10 分钟前才判定的轮询，重启后不该立刻又去试 IDLE——那正是要省掉的三圈")
		}
	})

	t.Run("冷静期从判定时刻算起，不从启动算起", func(t *testing.T) {
		// 这一条是上一条的反面，也是更容易写错的那个方向：如果冷静期从
		// 进程启动算，那么每重启一次就重新拉满 30 分钟，一台已经改好的
		// 服务器会被永远关在推送门外。
		var h idleHealth
		h.seed(PushModePoll, now.Add(-idleRetryAfter-time.Minute), now)
		if !h.idleWorthTrying(now) {
			t.Fatal("上次判定已经过了冷静期，重启后应该重新试一次 IDLE")
		}
	})

	t.Run("上次是推送、或者没学过：照常开工", func(t *testing.T) {
		for _, mode := range []string{PushModeIdle, ""} {
			var h idleHealth
			h.seed(mode, now.Add(-time.Hour), now)
			if !h.idleWorthTrying(now) {
				t.Fatalf("mode=%q 不该让它安静", mode)
			}
		}
	})

	t.Run("没有判定时间的旧行：当没学过", func(t *testing.T) {
		// push_checked_at 是这次迁移新加的，存量行是 NULL。
		var h idleHealth
		h.seed(PushModePoll, time.Time{}, now)
		if !h.idleWorthTrying(now) {
			t.Fatal("不知道什么时候判定的，就该重新试，而不是无限期安静")
		}
	})
}

// 只在结论变了的时候写库：IDLE 正常时每二十几分钟一圈，圈圈都写等于把一次读
// 变成一次写，而结论几乎从不变。
func TestOnlyWriteWhenModeChanges(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	var h idleHealth
	h.seed(PushModeIdle, now.Add(-time.Hour), now)
	if _, differs := h.changed(now); differs {
		t.Fatal("一直是推送，不该写库")
	}

	// 连着三圈被掐断 → 退回轮询。
	for i := 0; i < idleGiveUpAfter; i++ {
		h.noteIdleRun(now, time.Second, true)
	}
	mode, differs := h.changed(now)
	if !differs || mode != PushModePoll {
		t.Fatalf("被掐了三圈之后该记成轮询，拿到 mode=%q differs=%v", mode, differs)
	}

	h.stored = mode
	if _, differs := h.changed(now); differs {
		t.Fatal("刚写完，同一时刻不该再写一次")
	}

	// 冷静期过了，重新试成功 → 改回推送。
	later := now.Add(idleRetryAfter + time.Minute)
	mode, differs = h.changed(later)
	if !differs || mode != PushModeIdle {
		t.Fatalf("冷静期过了该记回推送，拿到 mode=%q differs=%v", mode, differs)
	}
}

// 一圈因为「收到新信」而正常结束，不等于这台服务器的 IDLE 可用。
func TestASuccessfulRoundDoesNotUndoTheGiveUp(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	var h idleHealth
	for i := 0; i < idleGiveUpAfter; i++ {
		h.noteIdleRun(now, time.Second, true)
	}
	// 放弃之后，即便记一圈"成功"（benign=false 表示不是被挂断的），冷静期
	// 也还在——modeNow 看的是冷静期，不是上一圈的成败。
	h.noteIdleRun(now, time.Second, false)
	if got := h.modeNow(now); got != PushModePoll {
		t.Fatalf("还在冷静期里就该是轮询，拿到 %q", got)
	}
}
