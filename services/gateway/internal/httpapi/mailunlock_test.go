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

	token, expires, err := store.Grant(ctx, tenantID, employeeID)
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
	if !store.Check(ctx, tenantID, employeeID, token) {
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
	if !store.Check(ctx, tenantID, employeeID, token) {
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
	if store.Check(ctx, tenantID, employeeID, token) {
		t.Fatal("已经不存在的凭证不该通过")
	}
	// 而且不能把它当成「没设到期时间」而重新上闹钟——那等于让一把过期的
	// 钥匙复活。
	if n, _ := rdb.Exists(ctx, key).Result(); n != 0 {
		t.Fatal("查一把不存在的钥匙不该把它写回 Redis")
	}

	// 空 token 直接拒，不打 Redis。
	if store.Check(ctx, tenantID, employeeID, "") {
		t.Fatal("空凭证不该通过")
	}
	// 别人的 token 说明不了这个人的邮箱——键里绑着身份。
	other, _, err := store.Grant(ctx, tenantID, employeeID+1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Revoke(ctx, tenantID, employeeID+1, other)
	if store.Check(ctx, tenantID, employeeID, other) {
		t.Fatal("拿别人的凭证不该通过")
	}
}
