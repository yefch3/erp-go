package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 同一个键的第二次提交**不会再打到后端**——这是幂等的全部承诺。
//
// 这些测试数的是后端被真正调了几次，不是响应对不对：响应对不对换成「每次都
// 真调一遍」也成立，只数调用次数才能钉住「第二次是重放的」。
//
// Run with: GATEWAY_TEST_REDIS=127.0.0.1:6380
func idemServer(t *testing.T, handler http.HandlerFunc) http.Handler {
	t.Helper()
	addr := os.Getenv("GATEWAY_TEST_REDIS")
	if addr == "" {
		t.Skip("set GATEWAY_TEST_REDIS to a reachable Redis")
	}
	s := &Server{Idem: NewIdemStore(addr, slog.New(slog.NewTextHandler(os.Stderr, nil)))}
	return s.idempotent(handler)
}

// 键要和别的测试跑不撞车。
func idemKey(t *testing.T) string {
	return t.Name() + "-" + time.Now().Format("150405.000000000")
}

func TestIdempotentReplaysInsteadOfReexecuting(t *testing.T) {
	var calls atomic.Int32
	h := idemServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// 每次执行给不同的响应体：重放的话两次拿到的一样，重复执行则不同。
		_, _ = w.Write([]byte(`{"call":` + itoa64(int64(n)) + `}`))
	})
	key := idemKey(t)

	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/receipt-transactions", strings.NewReader(`{}`))
		req.Header.Set("Idempotency-Key", key)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	first := do()
	second := do()
	if calls.Load() != 1 {
		t.Fatalf("后端应该只被调一次，实际 %d 次——第二次没有被重放", calls.Load())
	}
	if second.Body.String() != first.Body.String() {
		t.Fatalf("重放的响应该和第一次逐字一致：第一次 %q 第二次 %q",
			first.Body.String(), second.Body.String())
	}
	if second.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("重放要带标记头，排查时才分得清真新建和重放")
	}
	if first.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatal("第一次是真执行，不该带重放标记")
	}
}

func TestIdempotentDifferentKeysBothExecute(t *testing.T) {
	var calls atomic.Int32
	h := idemServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
		req.Header.Set("Idempotency-Key", idemKey(t)+"-"+itoa64(int64(i)))
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	if calls.Load() != 2 {
		t.Fatalf("不同的键是不同的操作，都该执行，实际 %d 次", calls.Load())
	}
}

func TestIdempotentNoKeyPassesThrough(t *testing.T) {
	var calls atomic.Int32
	h := idemServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	for i := 0; i < 2; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/x", nil))
	}
	if calls.Load() != 2 {
		t.Fatalf("没带键就是自愿不要保护，两次都该执行，实际 %d 次", calls.Load())
	}
}

// 失败不留底。用户在同一个对话框里改好字段重新提交——键没换——必须真的
// 再执行一次，而不是撞上重放的旧报错。
func TestIdempotentFailureIsNotReplayed(t *testing.T) {
	var calls atomic.Int32
	h := idemServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest) // 第一次：表单填错了
			return
		}
		w.WriteHeader(http.StatusOK) // 改好了再来
	})
	key := idemKey(t)
	do := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
		req.Header.Set("Idempotency-Key", key)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := do(); code != http.StatusBadRequest {
		t.Fatalf("第一次该是 400，实际 %d", code)
	}
	if code := do(); code != http.StatusOK {
		t.Fatalf("改好重试该真执行并成功，实际 %d", code)
	}
	if calls.Load() != 2 {
		t.Fatalf("失败之后的重试必须真执行，实际共 %d 次", calls.Load())
	}
}

// 双击：第一下还在跑的时候第二下到了。第二下要吃 409，不能排进去再写一单。
func TestIdempotentConcurrentDuplicateGets409(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	h := idemServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-release // 卡住，模拟慢写
		w.WriteHeader(http.StatusOK)
	})
	key := idemKey(t)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
		req.Header.Set("Idempotency-Key", key)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}()

	// 等第一下确实进了后端再点第二下。
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("第一下没进后端")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	req.Header.Set("Idempotency-Key", key)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("第一下还在跑时第二下该吃 409，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "IDEM_IN_FLIGHT") {
		t.Fatalf("要给前端认得的码，实际 %s", rec.Body.String())
	}

	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("后端只该被进一次，实际 %d 次", calls.Load())
	}
}

// GET 不设防：读操作重复无害，为它绕 Redis 一圈是纯开销。
func TestIdempotentIgnoresReads(t *testing.T) {
	var calls atomic.Int32
	h := idemServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	key := idemKey(t)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		req.Header.Set("Idempotency-Key", key)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	if calls.Load() != 2 {
		t.Fatalf("GET 带了键也不该被拦，实际 %d 次", calls.Load())
	}
}
