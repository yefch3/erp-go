// Package httpin 是邮件服务唯一的 HTTP 门面，只为 OnlyOffice Document Server
// 开着。
//
// 为什么邮件服务突然要一个 HTTP 口：Document Server 是一台独立的服务器，它
// 用 HTTP 取文件、用 HTTP 回存改动，不会说 gRPC。而这两件事都不能走前门
// （网关）——前门在公网上，而这是一个**会写文件**的口。
//
// 所以它只在 compose 网里监听，容器不往宿主机发布这个端口（见
// deploy/docker-compose.services.yml 里 mail 那一段的 expose）。公网到不了
// 这里，nginx 里也没有任何一条 location 指向它。
//
// 身份验两道（签名 + 我们自己签的 token），都在 app 层，见 officeedit.go。
// 这一层只负责把 HTTP 拆开、把结果拼回去。
package httpin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// MaxCallbackBody 是回调正文的上限。正文是一小段 JSON（状态、地址、几个
// 用户名），几 KB 顶天；不封顶的话，一个能碰到这个口的东西可以用一个
// 无限长的 body 把内存吃光。
const MaxCallbackBody = 1 << 20 // 1 MB

// Service 是这一层用得到的那两件事。写成接口是为了测试能替，也为了这里
// 看得出它只碰这两个方法。
type Service interface {
	ServeOfficeFile(ctx context.Context, req app.OfficeFileRequest) (app.OfficeFile, error)
	SaveOfficeEdit(ctx context.Context, req app.OfficeFileRequest, body []byte) error
}

// NewOfficeMux 组出这个口上的两条路。
func NewOfficeMux(svc Service, log *slog.Logger) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /office/file", func(w http.ResponseWriter, r *http.Request) {
		f, err := svc.ServeOfficeFile(r.Context(), reqOf(r))
		if err != nil {
			// 对外只说 403，不说是签名不对还是附件不在：这个口的对面是一台
			// 服务器，它读不懂理由，而理由本身是在告诉人怎么试下一次。
			log.Warn("refused an office file request", "err", err)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ct := f.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Length", strconv.Itoa(len(f.Bytes)))
		// 不缓存、不猜类型：Document Server 每次都该拿到当前这一版。
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(f.Bytes)
	})

	mux.HandleFunc("POST /office/save", func(w http.ResponseWriter, r *http.Request) {
		// cap+1 再判，和 readCapped / fetchEdited 一个写法：只截断不判的话，
		// 超长的正文会变成一段解不开的 JSON，报出来的是"正文不合法"而不是
		// "正文太大"——查起来差很远。
		body, err := io.ReadAll(io.LimitReader(r.Body, MaxCallbackBody+1))
		if err == nil && int64(len(body)) > MaxCallbackBody {
			err = errors.New("callback body exceeds the cap")
		}
		if err != nil {
			writeCallbackResult(w, log, err)
			return
		}
		writeCallbackResult(w, log, svc.SaveOfficeEdit(r.Context(), reqOf(r), body))
	})
	return mux
}

func reqOf(r *http.Request) app.OfficeFileRequest {
	return app.OfficeFileRequest{
		Token:         r.URL.Query().Get("t"),
		Authorization: r.Header.Get("Authorization"),
	}
}

// writeCallbackResult 按 Document Server 认的那套回话。
//
// **状态码永远是 200，成败写在 body 里的 error 上**——这是它定的协议，
// 不是我们的偏好。回非 200 它会当成网络问题重试，而回 200 带 error:1 它会
// 按"存失败"重试并最终放弃，两者的退避和放弃时机不一样。
//
// error:0 的意思是"我收下了"。收不下却回 0，人改的东西就此蒸发，而且
// Document Server 会把它从缓存里删掉——那时谁都救不回来。
func writeCallbackResult(w http.ResponseWriter, log *slog.Logger, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		log.Error("could not store an edited attachment", "err", err)
		_, _ = w.Write([]byte(`{"error":1}`))
		return
	}
	_, _ = w.Write([]byte(`{"error":0}`))
}
