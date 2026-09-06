package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// 邮箱解锁凭证用着就续期，和 ERP 登录会话同一个规矩。
//
// 从前这里只问「还在吗」、从不续期，于是有效期是从验证那一刻起的一段固定
// 时长：不管当天用得多勤，到点必掉，天天如此。业务的原话是「每天一到点就
// 要重新登录」。
//
// Run with: GATEWAY_TEST_REDIS=127.0.0.1:6380
func TestUnlockSlidesWhileInUse(t *testing.T) {
	addr := os.Getenv("GATEWAY_TEST_REDIS")
	if addr == "" {
		t.Skip("set GATEWAY_TEST_REDIS to a reachable Redis")
	}
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer func() { _ = rdb.Close() }()

	const ttl = 10 * time.Second
	store := NewUnlockStore(addr, ttl)
	const tenantID, employeeID = 991, 992

	token, expires, err := store.Grant(ctx, tenantID, employeeID, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Revoke(ctx, tenantID, employeeID, token)
	if expires != int(ttl.Seconds()) {
		t.Fatalf("首次授予该报满额有效期 %d 秒，实际 %d", int(ttl.Seconds()), expires)
	}
	key := store.key(tenantID, employeeID, token)

	// 还没用掉一半：查一次不该写 Redis，剩余时间基本不变。逐次续期会把
	// 邮件页每次翻页都变成一次写。
	if _, err := rdb.Expire(ctx, key, 8*time.Second).Result(); err != nil {
		t.Fatal(err)
	}
	if !checkOK(store.Check(ctx, tenantID, employeeID, token)) {
		t.Fatal("有效的凭证该通过")
	}
	left, err := rdb.TTL(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	if left > 8*time.Second {
		t.Fatalf("没用掉一半就不该续期，剩余从 8s 变成了 %v", left)
	}

	// 用掉一半之后：一次访问就把它续满，人不会在用着的时候被踢出来。
	if _, err := rdb.Expire(ctx, key, 3*time.Second).Result(); err != nil {
		t.Fatal(err)
	}
	if !checkOK(store.Check(ctx, tenantID, employeeID, token)) {
		t.Fatal("将要过期但还没过期的凭证仍然有效")
	}
	left, err = rdb.TTL(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	if left <= 3*time.Second {
		t.Fatalf("用掉一半后该续满，剩余仍是 %v", left)
	}

	// 真过期了就是过期，续期不能把死凭证救活。
	if _, err := rdb.Del(ctx, key).Result(); err != nil {
		t.Fatal(err)
	}
	if checkOK(store.Check(ctx, tenantID, employeeID, token)) {
		t.Fatal("已经不存在的凭证不该通过")
	}
	// 而且不能把它当成「没设到期时间」而重新上闹钟——那等于让一把过期的
	// 钥匙复活。
	if n, _ := rdb.Exists(ctx, key).Result(); n != 0 {
		t.Fatal("查一把不存在的钥匙不该把它写回 Redis")
	}

	// 空 token 直接拒，不打 Redis。
	if checkOK(store.Check(ctx, tenantID, employeeID, "")) {
		t.Fatal("空凭证不该通过")
	}
	// 别人的 token 说明不了这个人的邮箱——键里绑着身份。
	other, _, err := store.Grant(ctx, tenantID, employeeID+1, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Revoke(ctx, tenantID, employeeID+1, other)
	if checkOK(store.Check(ctx, tenantID, employeeID, other)) {
		t.Fatal("拿别人的凭证不该通过")
	}
}

// checkOK 把 Check 的两个返回值收成一个布尔，给只关心「过不过」的断言用。
func checkOK(_ int64, ok bool) bool { return ok }

// 退出一个信箱，别的箱照开。
//
// 这条是「点了一个退出之后全部都退了」那个毛病的反面。从前令牌是一个人
// 一把（键里只有租户和员工），所以「退出邮箱」这个按钮**只可能**是全退——
// 手上就那一把，撤了就什么都没了。
//
// 拆到箱这一级之后，两件事要同时成立：
//
//	· 退出 A，B 不受影响（这条测试的主张）
//	· 每把令牌只开自己那个箱（这里验的是"两把互相独立"）
//
// 2026-09-01 之前这里还写着「切到 B 不用重新输密码，靠验证时一次性把每个箱
// 的令牌都发下来」。那条口径被推翻了：一次验证只开刚验过的那一个箱，切到
// 没解锁过的箱会弹一次门。见 verifyMailbox 里那段注释。
func TestSigningOutOfOneMailboxLeavesTheOthersOpen(t *testing.T) {
	addr := os.Getenv("GATEWAY_TEST_REDIS")
	if addr == "" {
		t.Skip("set GATEWAY_TEST_REDIS")
	}
	ctx := context.Background()
	store := NewUnlockStore(addr, time.Minute)
	tenantID, employeeID := time.Now().UnixNano(), int64(660001)
	const boxA, boxB int64 = 4001, 4002

	tokA, _, err := store.Grant(ctx, tenantID, employeeID, boxA)
	if err != nil {
		t.Fatal(err)
	}
	tokB, _, err := store.Grant(ctx, tenantID, employeeID, boxB)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Revoke(ctx, tenantID, employeeID, tokA)
	defer store.Revoke(ctx, tenantID, employeeID, tokB)

	// 每把令牌自己知道开的是哪个箱——门那一层靠这个，而不是靠请求参数。
	if got, ok := store.Check(ctx, tenantID, employeeID, tokA); !ok || got != boxA {
		t.Fatalf("A 的令牌该开 A，拿到 %d/%v", got, ok)
	}
	if got, ok := store.Check(ctx, tenantID, employeeID, tokB); !ok || got != boxB {
		t.Fatalf("B 的令牌该开 B，拿到 %d/%v", got, ok)
	}

	// 退出 A。
	store.Revoke(ctx, tenantID, employeeID, tokA)

	if _, ok := store.Check(ctx, tenantID, employeeID, tokA); ok {
		t.Error("退出了 A，A 的令牌还活着")
	}
	if got, ok := store.Check(ctx, tenantID, employeeID, tokB); !ok || got != boxB {
		t.Error("退出 A 把 B 也退了——这正是要修的那个毛病：" +
			"点一个退出，全部都退了")
	}
}

// 值为 "1" 的令牌开的是 1 号信箱，不是「不限信箱」。
//
// 8 月 31 日改版前的旧令牌值写死 "1"，改版后这里曾把 "1" 当成旧令牌放行成
// 不限箱——可 1 号信箱的新令牌值也是 "1"。那个箱的主人从那天起收件箱里
// 列的一直是他全部信箱的信，左边高亮 Gmail、右边混着 163 的提醒，没有任何
// 报错。这条钉住：值是几就是几号箱，"1" 没有第二种读法。
func TestValueOneMeansMailboxOneNotAllMailboxes(t *testing.T) {
	addr := os.Getenv("GATEWAY_TEST_REDIS")
	if addr == "" {
		t.Skip("set GATEWAY_TEST_REDIS")
	}
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	store := NewUnlockStore(addr, time.Minute)
	tenantID, employeeID := time.Now().UnixNano(), int64(660002)

	tok, _, err := store.Grant(ctx, tenantID, employeeID, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Revoke(ctx, tenantID, employeeID, tok)

	acct, ok := store.Check(ctx, tenantID, employeeID, tok)
	if !ok {
		t.Fatal("1 号箱的令牌被判成无效")
	}
	if acct != 1 {
		t.Errorf("1 号箱的令牌该开 1 号箱，拿到 %d（0 = 不限箱，就是那个把全部信箱混在一起的毛病）", acct)
	}
	// 一个箱都没绑的人那把「不限箱」通行证仍然是 0。
	none, _, err := store.Grant(ctx, tenantID, employeeID, accountAll)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Revoke(ctx, tenantID, employeeID, none)
	if acct, ok := store.Check(ctx, tenantID, employeeID, none); !ok || acct != accountAll {
		t.Errorf("不限箱通行证应该是 0，拿到 %d ok=%v", acct, ok)
	}
}
