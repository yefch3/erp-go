package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/sgao19/erp-go/pkg/grpcx"
)

// HTTP 写入的幂等。
//
// 没有它，同一个动作到达两次就写两次：按钮的防连点漏了一处、或者请求超时后
// 用户手动重试（服务端其实已经成功，只是响应丢在了路上）——于是两张一样的
// 单、两笔一样的付款。Kafka 那条路早有幂等（按事件 id 去重），HTTP 一直没有。
//
// 做法是**凭据换重放**：前端在一次「表单会话」开始时生成一个键
// （Idempotency-Key 头），提交失败重试时带同一个键；网关第一次放行并把成功
// 响应存进 Redis，第二次直接把存的响应原样还给它，**后端根本不会再被调一次**。
//
// 三条边界，每一条都有理由：
//
//   - **只存成功（2xx）。** 存 4xx 的话，用户在同一个对话框里改好字段重新提交
//     ——键没换——会被重放那个旧的报错，怎么改都过不去。失败就删占位，
//     放行重试。
//   - **同键并发给 409**，不排队等。第二个请求等第一个的结果要占着连接空转，
//     而「正在处理，请稍候」是句用户能懂的话。
//   - **Redis 挂了放行并记 WARN**，不拦业务。挂掉的那段时间退回到今天的现状
//     （没有幂等），这比「Redis 一挂全公司不能开单」的代价小；WARN 是因为
//     该说话的地方必须说话。
//
// 键的隔离带着公司和员工：两个人（或两家公司）撞出同一个 UUID 的概率可以
// 忽略，但隔离让「重放别人的响应」这件事在结构上就不可能，而不是靠概率。
type IdemStore struct {
	rdb *redis.Client
	log *slog.Logger
}

// NewIdemStore 连到网关已有的那台 Redis（邮箱解锁、实时推送同一台）。
func NewIdemStore(addr string, log *slog.Logger) *IdemStore {
	return &IdemStore{rdb: redis.NewClient(&redis.Options{Addr: addr}), log: log}
}

const (
	// 占位的存活时间。要盖住最慢的一次写（导入、生成 PDF 都在别的路由，
	// 这里的写都是秒级），又不能在网关崩溃后把这个键锁死太久。
	idemPendingTTL = 60 * time.Second
	// 成功响应的存活时间。重试发生在秒到分钟级；存一天是给「用户走开又回来
	// 点了刷新重发」留余量，代价只是 Redis 里多放几条小 JSON。
	idemResultTTL = 24 * time.Hour
	// 超过这个大小就不存（也就重放不了）。创建类响应都是小 JSON，会超的是
	// 导出下载一类，而那些本来就不该带幂等键。
	idemMaxBody = 256 * 1024
	// 键本身的长度上限。键是前端生成的 UUID，128 足够；不设限的话这个头
	// 就成了往 Redis 里塞任意大 key 的口子。
	idemMaxKeyLen = 128

	idemPendingMark = "\x00P"
)

// storedResponse 是重放时要还原的全部东西。
type storedResponse struct {
	Status      int    `json:"s"`
	ContentType string `json:"ct"`
	Body        []byte `json:"b"`
}

// idempotent 是挂在认证之后的中间件。没带键的请求原样通过——它是自愿加入
// 的：想要保护的表单带键，别的请求零成本路过。
func (s *Server) idempotent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || !mutating(r.Method) || s.Idem == nil {
			next.ServeHTTP(w, r)
			return
		}
		if len(key) > idemMaxKeyLen {
			s.writeError(w, http.StatusBadRequest, "IDEM_KEY_TOO_LONG",
				"幂等键太长")
			return
		}
		op, _ := grpcx.OperatorFromContext(r.Context())
		redisKey := "idem:" + itoa64(op.TenantID) + ":" + itoa64(op.EmployeeID) +
			":" + r.Method + ":" + r.URL.Path + ":" + key
		ctx := r.Context()

		acquired, err := s.Idem.rdb.SetNX(ctx, redisKey, idemPendingMark, idemPendingTTL).Result()
		if err != nil {
			// Redis 不在。放行——这段时间退回到没有幂等的现状——但必须
			// 说出来，不然「防重开着」和「防重早就没在防」看起来一模一样。
			s.Idem.log.Warn("幂等存储不可用，这个请求没有防重保护",
				"path", r.URL.Path, "err", err.Error())
			next.ServeHTTP(w, r)
			return
		}
		if !acquired {
			stored, err := s.Idem.rdb.Get(ctx, redisKey).Bytes()
			switch {
			case err == redis.Nil:
				// 键在 SETNX 和 GET 之间消失了：前一个持有者失败后删了它。
				// 重试一次抢占；再抢不到就按并发处理。
				acquired, err2 := s.Idem.rdb.SetNX(ctx, redisKey, idemPendingMark, idemPendingTTL).Result()
				if err2 != nil || !acquired {
					s.writeError(w, http.StatusConflict, "IDEM_IN_FLIGHT",
						"同一个操作正在处理，请稍等片刻再试")
					return
				}
			case err != nil:
				s.Idem.log.Warn("幂等存储读取失败，这个请求没有防重保护",
					"path", r.URL.Path, "err", err.Error())
				next.ServeHTTP(w, r)
				return
			case bytes.Equal(stored, []byte(idemPendingMark)):
				// 第一个还在跑。双击的第二下会落在这里。
				s.writeError(w, http.StatusConflict, "IDEM_IN_FLIGHT",
					"同一个操作正在处理，请稍等片刻再试")
				return
			default:
				var resp storedResponse
				if json.Unmarshal(stored, &resp) == nil {
					// 重放。带上标记头：排查「这单是真新建的还是重放的」
					// 时，抓包一眼能看出来。
					w.Header().Set("Content-Type", resp.ContentType)
					w.Header().Set("Idempotency-Replayed", "true")
					w.WriteHeader(resp.Status)
					_, _ = w.Write(resp.Body)
					return
				}
				// 存的东西读不懂：删掉重来，别让一条坏记录永远挡着这个键。
				_ = s.Idem.rdb.Del(ctx, redisKey).Err()
				next.ServeHTTP(w, r)
				return
			}
		}

		// 我们是第一个：真正执行，并把响应抄一份。
		rec := &responseTap{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.Idem.finish(ctx, redisKey, rec)
	})
}

// finish 决定这次执行的结果要不要留底。
func (i *IdemStore) finish(ctx context.Context, redisKey string, rec *responseTap) {
	// 只存成功。失败删占位放行重试——用户改好表单再提交，键没换，不能
	// 让他撞上重放的旧报错。
	if rec.status < 200 || rec.status >= 300 || rec.overflow {
		if err := i.rdb.Del(ctx, redisKey).Err(); err != nil {
			// 删不掉的占位 60 秒后自己过期，期间重试会吃 409。说一声，
			// 让「为什么我重试被拒了一分钟」查得到原因。
			i.log.Warn("幂等占位没能清除，重试会被挡最多 60 秒",
				"key", redisKey, "err", err.Error())
		}
		return
	}
	payload, err := json.Marshal(storedResponse{
		Status:      rec.status,
		ContentType: rec.Header().Get("Content-Type"),
		Body:        rec.buf.Bytes(),
	})
	if err == nil {
		err = i.rdb.Set(ctx, redisKey, payload, idemResultTTL).Err()
	}
	if err != nil {
		// 存不进去：这一次已经成功返回了，只是将来的重试挡不住。
		i.log.Warn("幂等结果没能存下，重复提交将不会被拦",
			"key", redisKey, "err", err.Error())
	}
}

// responseTap 一边把响应写给客户端，一边留一份底。超过上限就停止留底
// （overflow），那种响应不参与重放。
type responseTap struct {
	http.ResponseWriter
	status   int
	buf      bytes.Buffer
	overflow bool
}

func (t *responseTap) WriteHeader(code int) {
	t.status = code
	t.ResponseWriter.WriteHeader(code)
}

func (t *responseTap) Write(b []byte) (int, error) {
	if !t.overflow {
		if t.buf.Len()+len(b) > idemMaxBody {
			t.overflow = true
			t.buf.Reset()
		} else {
			t.buf.Write(b)
		}
	}
	return t.ResponseWriter.Write(b)
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [21]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
