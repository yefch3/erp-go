package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 收件箱列表怎么排一行：一条会话一行，还是一封信一行。
//
// 我们从一开始是前者（Gmail 那一派）。263 和 Foxmail 默认是后者，用惯那边
// 的人看我们的列表会觉得信少了。两种都不是错的，所以这是个开关，跟人走，
// 不跟公司走——和签名、模板同一条口径（见迁移 00073）。
type ListMode string

const (
	// ListModeThread 按会话合并。没设置过的人是这一档。
	ListModeThread ListMode = "THREAD"
	// ListModeMessage 一封一行。
	ListModeMessage ListMode = "MESSAGE"
)

// merged 是这个档要不要把会话合起来。写成一个方法而不是到处比字符串：
// 加第三档时（比如 Outlook 那种「只在某些文件夹里合并」）要改的是这里，
// 不是散落各处的 `== "THREAD"`。
func (m ListMode) merged() bool { return m != ListModeMessage }

// normalizeListMode 把库里或调用方给的值收成两档之一。
//
// 认不出来的值**退回默认档**而不是报错，和 normalizeView 同一个取舍：这是
// 个显示偏好，最坏的后果是看到一直以来的样子。设置那一侧是另一回事，见
// SetListMode——那里认不出来必须报错，否则人点了保存、屏幕没变、也没有任何
// 提示。
func normalizeListMode(s string) ListMode {
	if ListMode(s) == ListModeMessage {
		return ListModeMessage
	}
	return ListModeThread
}

// ListMode 是这个人的列表模式。
//
// 没设置过的人库里没有行，那不是错误——就是默认档。查不动库时也给默认档：
// 一个显示偏好不值得让整个收件箱打不开。
func (s *Service) ListMode(ctx context.Context, tenantID, employeeID int64) ListMode {
	mode, err := s.q.GetMailListMode(ctx, store.GetMailListModeParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.Warn("could not read the mail list mode", "employee", employeeID, "err", err)
		}
		return ListModeThread
	}
	return normalizeListMode(mode)
}

// SetListMode 改这个人的列表模式。
//
// 这里认不出来的值要报错，不能像读取那样悄悄退回默认：人刚点了「一封一行」，
// 保存回来还是合并的，而屏幕上没有任何东西说明为什么。
func (s *Service) SetListMode(ctx context.Context, tenantID, employeeID int64, mode string) (ListMode, error) {
	m := ListMode(mode)
	if m != ListModeThread && m != ListModeMessage {
		return "", apierr.Invalid("MAIL_LIST_MODE_INVALID", "列表模式只能是 THREAD 或 MESSAGE")
	}
	if err := s.q.SetMailListMode(ctx, store.SetMailListModeParams{
		TenantID: tenantID, EmployeeID: employeeID, ListMode: string(m),
	}); err != nil {
		return "", err
	}
	return m, nil
}
