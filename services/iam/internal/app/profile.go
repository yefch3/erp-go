package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// 「我的资料」——员工看自己、改自己。
//
// 这一组的调用者一律来自认证上下文，接口里没有「员工 id」这个入参，所以
// 「看别人的资料」不是一个被拒绝的请求，而是一个**表达不出来的**请求。
// 这比在每个方法里写一遍 if id != op.EmployeeID 可靠：漏写一处就是越权，
// 而漏写一个不存在的参数是做不到的。

// Files 是头像要用的对象存储。可选依赖：没接就只是传不了头像，
// 员工资料的其余部分照常工作——IAM 在这次之前从来不碰文件，
// 不该因为对象存储没配就起不来。
type Files interface {
	PresignPut(ctx context.Context, key string) (url string, expiresInSeconds int32, err error)
	PresignGet(ctx context.Context, key string) (string, error)
	Stat(ctx context.Context, key string) (size int64, contentType string, err error)
	Remove(ctx context.Context, key string) error
}

// UseFiles 接上对象存储。用 setter 而不是加构造参数，是因为 app.New 被十几处
// 测试调用着，为一个可选依赖改它们的签名不划算——shipping 的 UseReminderNotifier
// 是同样的取舍。
func (s *Service) UseFiles(f Files) { s.files = f }

const (
	// 头像大小上限。一张 512×512 的 JPEG 在 100 KB 上下，2 MB 是「手机原图直接
	// 传上来也放得下」的余量，同时挡住把这个口子当网盘用。
	avatarMaxBytes = 2 << 20
)

// 允许的头像格式。白名单而不是黑名单：浏览器认得的图片格式每年都在变，
// 而「哪几种我们愿意存」是我们自己说了算的。
// SVG 故意不在里面——它能带脚本，而头像会被当图片直接嵌进页面。
var avatarContentTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// MyProfileView 是员工能看到的关于自己的东西。
//
// 少了两样，都是故意的：
//   - remark 是主管写给管理层的评价，本人不可见
//   - leave_date 可能是管理员提前录进去、还没跟人谈过的
//
// 不返回，不是「返回了但前端不显示」——前端藏字段等于没藏。
type MyProfileView struct {
	ID             int64
	Code           string
	Name           string
	EnglishName    string
	DepartmentName string
	Position       string
	Email          string
	Phone          string
	Status         string
	ManagerName    string
	HireDate       string
	AvatarKey      string
	// 短命的签名地址，没有头像或没接存储时为空。不进任何缓存——见 proto 注释。
	AvatarURL     string
	Version       int32
	EmailVerified bool
}

// GetMyProfile 读调用者自己的资料。
func (s *Service) GetMyProfile(ctx context.Context, tenantID, employeeID int64) (MyProfileView, error) {
	if employeeID == 0 {
		return MyProfileView{}, apierr.Unauthorized("IAM_NO_IDENTITY", "无法确认当前登录身份，请重新登录")
	}
	row, err := s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: employeeID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MyProfileView{}, apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在")
		}
		return MyProfileView{}, err
	}
	return s.withAvatarURL(ctx, myProfileOf(row)), nil
}

// UpdateMyProfileInput 就是白名单本身。加字段之前先回到方案里那张权限表——
// 这个结构体多一个字段，就等于员工多能改一样东西。
type UpdateMyProfileInput struct {
	EnglishName     string
	Phone           string
	ExpectedVersion int32
}

// UpdateMyProfile 保存员工自己能改的那两项。
//
// 带版本号，和管理员那条路共用同一把锁：管理员正在改这个人的资料时员工按了
// 保存，应该是「有人改过了，请刷新」，而不是谁后写谁赢。
func (s *Service) UpdateMyProfile(ctx context.Context, tenantID, employeeID int64, in UpdateMyProfileInput) (MyProfileView, error) {
	if employeeID == 0 {
		return MyProfileView{}, apierr.Unauthorized("IAM_NO_IDENTITY", "无法确认当前登录身份，请重新登录")
	}
	if in.ExpectedVersion < 1 {
		return MyProfileView{}, apierr.Invalid("IAM_EMP_FIELDS_REQUIRED", "缺少资料版本号，请刷新后重试")
	}
	if err := validateEmployeePhone(in.Phone); err != nil {
		return MyProfileView{}, err
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: employeeID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在")
			}
			return err
		}
		updated, err := q.UpdateOwnProfile(ctx, store.UpdateOwnProfileParams{
			EnglishName:     strings.TrimSpace(in.EnglishName),
			Phone:           strings.TrimSpace(in.Phone),
			TenantID:        tenantID,
			ID:              employeeID,
			ExpectedVersion: in.ExpectedVersion,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.Conflict("IAM_EMP_VERSION_CONFLICT", "资料已被其他人修改，请刷新后重试")
			}
			return err
		}
		// 自助修改照样进变更记录，操作人就是本人。审计要能回答「这个电话是谁
		// 什么时候改的」，而「员工自己改的」是个完全合理的答案，不是例外。
		return q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: tenantID, EntityType: "EMPLOYEE", EntityID: employeeID, Action: "SELF_UPDATE",
			BeforeData: snapshotJSON(before), AfterData: snapshotJSON(updated), OperatorID: employeeID,
		})
	})
	if err != nil {
		return MyProfileView{}, err
	}
	return s.GetMyProfile(ctx, tenantID, employeeID)
}

// ---------------------------------------------------------------- 头像

// PresignAvatarUpload 发一个短命的上传地址，浏览器把图片直接 PUT 到对象存储，
// 不经过我们的服务。
//
// 返回的 key 要原样回传给 SetAvatar——**而 SetAvatar 会重新校验这个 key 是不是
// 我们发出去的形状**。不校验的话，前端可以回传桶里任意一个对象的 key（比如某份
// 合同 PDF），头像地址就成了任意文件的读取口。
func (s *Service) PresignAvatarUpload(ctx context.Context, tenantID, employeeID int64, fileName, contentType string) (key, url string, expires int32, err error) {
	if s.files == nil {
		return "", "", 0, apierr.Invalid("IAM_FILES_UNAVAILABLE", "文件存储未配置，暂时无法上传头像")
	}
	if employeeID == 0 {
		return "", "", 0, apierr.Unauthorized("IAM_NO_IDENTITY", "无法确认当前登录身份，请重新登录")
	}
	ext, ok := avatarContentTypes[strings.ToLower(strings.TrimSpace(contentType))]
	if !ok {
		return "", "", 0, apierr.Invalid("IAM_AVATAR_TYPE", "头像只支持 JPG、PNG 或 WebP")
	}
	// 后缀由 content-type 决定，不取文件名里的——文件名是用户给的，
	// 「照片.png」里装一个别的东西是最老的一招。
	key = avatarKey(tenantID, employeeID, ext)
	url, expires, err = s.files.PresignPut(ctx, key)
	if err != nil {
		return "", "", 0, err
	}
	return key, url, expires, nil
}

// SetAvatar 在图片真的传上去之后，把 key 落到员工记录上。
//
// 三道校验，每一道都对应一种「浏览器说的话不能全信」：
//  1. key 必须是我们给这个员工发的那个前缀——否则等于任意文件读取口
//  2. 对象必须真的存在——前端可能 PUT 失败了还照样回调
//  3. 实际大小和类型以存储里的为准，不以前端报的为准
func (s *Service) SetAvatar(ctx context.Context, tenantID, employeeID, operatorID int64, key string) (string, error) {
	if s.files == nil {
		return "", apierr.Invalid("IAM_FILES_UNAVAILABLE", "文件存储未配置，暂时无法上传头像")
	}
	if employeeID == 0 {
		return "", apierr.Invalid("IAM_EMP_FIELDS_REQUIRED", "缺少员工")
	}
	key = strings.TrimSpace(key)
	if key != "" {
		if !strings.HasPrefix(key, avatarPrefix(tenantID, employeeID)) {
			return "", apierr.Invalid("IAM_AVATAR_KEY_FOREIGN", "头像文件不属于这名员工")
		}
		size, ct, err := s.files.Stat(ctx, key)
		if err != nil {
			return "", apierr.Invalid("IAM_AVATAR_MISSING", "头像还没有上传完成，请重试")
		}
		if size > avatarMaxBytes {
			return "", apierr.Invalid("IAM_AVATAR_TOO_LARGE", "头像不能超过 2 MB")
		}
		if _, ok := avatarContentTypes[strings.ToLower(ct)]; !ok {
			return "", apierr.Invalid("IAM_AVATAR_TYPE", "头像只支持 JPG、PNG 或 WebP")
		}
	}
	before, err := s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: employeeID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在")
		}
		return "", err
	}
	updated, err := s.q.SetEmployeeAvatar(ctx, store.SetEmployeeAvatarParams{
		AvatarKey: key, TenantID: tenantID, ID: employeeID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在")
		}
		return "", err
	}
	if err := s.q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
		TenantID: tenantID, EntityType: "EMPLOYEE", EntityID: employeeID, Action: "AVATAR",
		BeforeData: snapshotJSON(before), AfterData: snapshotJSON(updated), OperatorID: operatorID,
	}); err != nil {
		return "", err
	}
	// 旧图删掉。删不掉不算失败——头像已经换好了，留一个没人引用的对象只是浪费
	// 几十 KB；为它回滚一次成功的修改才是本末倒置。但要说一声，不然桶会悄悄涨。
	if before.AvatarKey != "" && before.AvatarKey != key {
		if err := s.files.Remove(ctx, before.AvatarKey); err != nil {
			s.log.Warn("旧头像没能删除，对象会一直留在桶里",
				"employee", employeeID, "key", before.AvatarKey, "err", err.Error())
		}
	}
	return key, nil
}

// AvatarURLs 把一批员工的头像 key 换成短命的可访问地址。
//
// 批量而不是一个个来：员工列表和组织架构图一次要显示几十上百张头像，
// 逐个签名就是逐个往返。没有头像的人直接不出现在结果里——用「缺席」表示
// 「没有」，比返回空串让调用方再判断一次少一层。
func (s *Service) AvatarURLs(ctx context.Context, tenantID int64, employeeIDs []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if s.files == nil || len(employeeIDs) == 0 {
		return out, nil
	}
	rows, err := s.q.ListEmployeeAvatars(ctx, store.ListEmployeeAvatarsParams{
		TenantID: tenantID, Ids: employeeIDs,
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.AvatarKey == "" {
			continue
		}
		url, err := s.files.PresignGet(ctx, r.AvatarKey)
		if err != nil {
			// 一张图签不出来，不该让整页人都没有头像。跳过并记一声。
			s.log.Warn("头像地址签名失败，这个人会显示成没有头像",
				"employee", r.ID, "key", r.AvatarKey, "err", err.Error())
			continue
		}
		out[r.ID] = url
	}
	return out, nil
}

// ---------------------------------------------------------------- 组织架构图

// OrgMemberView 是架构图上的一个人。
type OrgMemberView struct {
	ID             int64
	Code           string
	Name           string
	EnglishName    string
	Position       string
	Status         string
	ManagerID      int64
	DepartmentID   int64
	DepartmentName string
	AvatarKey      string
	AvatarURL      string
	LeaveDate      string
}

// OrgChart 一次返回画两棵树要的全部数据。
//
// 部门单独返回而不是从员工反推：一个还没进人的部门也该在图上——
// 它是刚成立还是被掏空了，正是看图的人想知道的事，而从员工反推会让它凭空消失。
func (s *Service) OrgChart(ctx context.Context, tenantID int64) ([]OrgMemberView, []store.Department, error) {
	rows, err := s.q.ListOrgChartMembers(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	depts, err := s.q.ListDepartments(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	members := make([]OrgMemberView, 0, len(rows))
	for _, r := range rows {
		m := OrgMemberView{
			ID: r.ID, Code: r.Code, Name: r.Name, EnglishName: r.EnglishName,
			Position: r.Position, Status: r.Status,
			ManagerID: derefID(r.ManagerID), DepartmentID: r.DepartmentID,
			DepartmentName: r.DepartmentName, AvatarKey: r.AvatarKey,
			LeaveDate: dateOnly(r.LeaveDate),
		}
		// 头像地址逐个签。这是纯本地的 HMAC 计算，不走网络，三百个也就是
		// 几毫秒——但真出错了要跳过而不是让整张图失败：少一张照片是小事，
		// 打不开架构图是大事。
		if s.files != nil && m.AvatarKey != "" {
			if url, err := s.files.PresignGet(ctx, m.AvatarKey); err == nil {
				m.AvatarURL = url
			} else {
				s.log.Warn("架构图里这个人的头像地址签名失败，会显示成没有头像",
					"employee", m.ID, "err", err.Error())
			}
		}
		members = append(members, m)
	}
	return members, depts, nil
}

func derefID(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// ---------------------------------------------------------------- 内部

// avatarPrefix 是一个员工的头像目录。SetAvatar 拿它做前缀校验，
// 所以它必须只由服务端算得出来的东西拼成，不能有任何来自请求的部分。
func avatarPrefix(tenantID, employeeID int64) string {
	return fmt.Sprintf("avatars/t%d/e%d/", tenantID, employeeID)
}

// avatarKey 每次上传都换一个随机名。覆盖同名会撞上 CDN 和浏览器缓存——
// 换了照片却还显示旧的，是个查起来很烦、看起来像玄学的问题。
func avatarKey(tenantID, employeeID int64, ext string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return avatarPrefix(tenantID, employeeID) + hex.EncodeToString(buf) + ext
}

// withAvatarURL 给视图补一个可直接显示的地址。
//
// 签不出来不算失败：那只是这一次少一张照片，而资料页的其余部分照常有用。
// 但要记一声——头像整片消失和「大家都没传过照片」看起来一模一样。
func (s *Service) withAvatarURL(ctx context.Context, v MyProfileView) MyProfileView {
	if s.files == nil || v.AvatarKey == "" {
		return v
	}
	url, err := s.files.PresignGet(ctx, v.AvatarKey)
	if err != nil {
		s.log.Warn("头像地址签名失败，这次会显示成没有头像",
			"employee", v.ID, "key", v.AvatarKey, "err", err.Error())
		return v
	}
	v.AvatarURL = url
	return v
}

func myProfileOf(e store.GetEmployeeRow) MyProfileView {
	return MyProfileView{
		ID: e.ID, Code: e.Code, Name: e.Name, EnglishName: e.EnglishName,
		DepartmentName: e.DepartmentName, Position: e.Position,
		Email: e.Email, Phone: e.Phone, Status: e.Status,
		ManagerName: e.ManagerName, HireDate: dateOnly(e.HireDate),
		AvatarKey: e.AvatarKey, Version: e.Version,
		EmailVerified: e.EmailVerifiedAt.Valid,
	}
}

// dateOnly 把可空日期变成 YYYY-MM-DD，没有就是空串。grpcin 里有个同样的
// dateText，但那是适配层的事；这里返回的是给业务用的视图，不该反过来依赖它。
func dateOnly(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}
