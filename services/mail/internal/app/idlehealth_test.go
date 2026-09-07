package app

import (
	"testing"
	"time"
)

// 一台每分钟掐一次 IDLE 的服务器（263 实测 66 秒），连着几圈之后就不该再用
// IDLE 了——再重连下去只是每分钟重新登录一次，一天一千多次。
func TestAHostThatKeepsCuttingIdleIsLeftToThePoller(t *testing.T) {
	var h idleHealth
	now := time.Now()

	// 前两圈：记下来，但还给它机会——偶尔一次抖动不算数。
	for i := 1; i < idleGiveUpAfter; i++ {
		if !h.noteIdleRun(now, 66*time.Second, true) {
			t.Fatalf("第 %d 圈就放弃了，太早：偶尔一次抖动不该停掉推送", i)
		}
		if !h.idleWorthTrying(now) {
			t.Fatalf("第 %d 圈之后不该进冷静期", i)
		}
	}
	// 第三圈：够了。
	if h.noteIdleRun(now, 66*time.Second, true) {
		t.Fatalf("连着 %d 圈都撑不住，应该停掉 IDLE", idleGiveUpAfter)
	}
	if h.idleWorthTrying(now) {
		t.Error("应该进冷静期，交给轮询")
	}
	if h.idleWorthTrying(now.Add(idleRetryAfter - time.Minute)) {
		t.Error("冷静期没到就又去连了")
	}
	if !h.idleWorthTrying(now.Add(idleRetryAfter + time.Second)) {
		t.Error("冷静期过了要再试一次——服务器会改配置，网络会变好")
	}
}

// 撑得住的服务器（Gmail、126 实测一次没掉）永远不该被停掉推送。
func TestAHealthyHostKeepsItsPush(t *testing.T) {
	var h idleHealth
	now := time.Now()
	for i := 0; i < 50; i++ {
		// 正常走完一圈：到点主动续命，不是被挂断。
		if !h.noteIdleRun(now, IdleRestartEvery, false) {
			t.Fatalf("第 %d 圈：撑得住的服务器不该被停掉推送", i)
		}
	}
	if !h.idleWorthTrying(now) {
		t.Error("健康的服务器不该进冷静期")
	}
}

// 收到新信而结束的一圈，哪怕只有几秒也是成功——那正是 IDLE 该有的样子。
func TestASecondsLongRunThatFoundMailIsASuccess(t *testing.T) {
	var h idleHealth
	now := time.Now()
	for i := 0; i < idleGiveUpAfter+2; i++ {
		if !h.noteIdleRun(now, 3*time.Second, false) {
			t.Fatal("有新信而结束的一圈不该算不合格——那是推送在起作用")
		}
	}
	if !h.idleWorthTrying(now) {
		t.Error("忙碌的信箱反而被停了推送")
	}
}

// 好一圈就把计数清零：偶尔被挂断一次，不该攒着算总账。
func TestOneGoodRunForgivesTheEarlierDrops(t *testing.T) {
	var h idleHealth
	now := time.Now()
	for i := 1; i < idleGiveUpAfter; i++ {
		h.noteIdleRun(now, 66*time.Second, true)
	}
	h.noteIdleRun(now, IdleRestartEvery, false) // 一圈正常的
	for i := 1; i < idleGiveUpAfter; i++ {
		if !h.noteIdleRun(now, 66*time.Second, true) {
			t.Fatalf("清零之后又数了 %d 圈就放弃，说明计数没清", i)
		}
	}
}

// 撑够时间之后被挂断，是正常的：263 撑不住是因为 66 秒，不是因为被挂断。
func TestALongRunEndingInADropIsFine(t *testing.T) {
	var h idleHealth
	now := time.Now()
	for i := 0; i < idleGiveUpAfter+3; i++ {
		if !h.noteIdleRun(now, idleTooShort+time.Second, true) {
			t.Fatal("撑够了时间的一圈不该算不合格")
		}
	}
}
