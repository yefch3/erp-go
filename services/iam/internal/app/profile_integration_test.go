package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// 「我的资料」这一组要钉住的，不是「保存能成功」，而是几件**不能发生**的事：
//
//   - 备注不能出现在员工看自己的返回里（它是主管写给管理层的评价）
//   - 离职日期同理——可能是管理员提前录进去、还没跟人谈过的
//   - 自助保存只能动那两个字段，别的一个都不能被顺带改掉
//   - 头像的 key 必须属于这个员工，否则头像地址就成了任意文件读取口
//
// 前两条在类型上就成立（MyProfileView 里没有那两个字段），但测试仍然写——
// 类型可以被人加字段，测试会在那一刻红。

func profileTestService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	dsn := os.Getenv("IAM_TEST_DSN")
	if dsn == "" {
		t.Skip("set IAM_TEST_DSN to a migrated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return New(pool, "test-secret", time.Hour, log), ctx
}

// seedProfileEmployee 造一个带备注和离职日期的员工，好让「看不见」这件事可被证伪。
func seedProfileEmployee(t *testing.T, s *Service, ctx context.Context) store.GetEmployeeRow {
	t.Helper()
	stamp := time.Now().UnixNano()
	dept, err := s.q.ListDepartments(ctx, 1)
	if err != nil || len(dept) == 0 {
		t.Skip("这个库里没有部门，跳过")
	}
	created, err := s.CreateEmployee(ctx, 1, CreateEmployeeInput{
		Code:         fmt.Sprintf("PF%d", stamp%1_000_000_000),
		Name:         fmt.Sprintf("资料测试-%d", stamp%100000),
		DepartmentID: dept[0].ID,
		Email:        fmt.Sprintf("pf%d@example.test", stamp),
		Phone:        "13800000000",
		Remark:       "这条备注员工不该看见",
		OperatorID:   1,
	})
	if err != nil {
		t.Fatalf("造员工失败: %v", err)
	}
	// 离职日期只能通过管理员那条路写，正好也验证了它不在自助白名单里。
	if _, err := s.UpdateEmployee(ctx, 1, UpdateEmployeeInput{
		ID: created.ID, Code: created.Code, Name: created.Name,
		DepartmentID: created.DepartmentID, Email: created.Email, Phone: created.Phone,
		Remark: "这条备注员工不该看见", LeaveDate: "2030-12-31",
		ExpectedVersion: created.Version, OperatorID: 1,
	}); err != nil {
		t.Fatalf("补离职日期失败: %v", err)
	}
	row, err := s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: 1, ID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestMyProfileNeverCarriesTheRemarkOrTheLeaveDate(t *testing.T) {
	s, ctx := profileTestService(t)
	emp := seedProfileEmployee(t, s, ctx)

	// 先确认库里**确实有**这两样，否则下面的断言是在证明空气。
	if emp.Remark == "" {
		t.Fatal("前置条件不成立：备注是空的，这个测试证明不了任何事")
	}
	if !emp.LeaveDate.Valid {
		t.Fatal("前置条件不成立：离职日期是空的")
	}

	view, err := s.GetMyProfile(ctx, 1, emp.ID)
	if err != nil {
		t.Fatalf("读自己的资料失败: %v", err)
	}
	// 逐字段扫一遍整个视图，而不是只看已知的几个：将来有人加了字段并顺手
	// 把备注塞进去，这条也会红。
	for name, got := range map[string]string{
		"Code": view.Code, "Name": view.Name, "EnglishName": view.EnglishName,
		"DepartmentName": view.DepartmentName, "Position": view.Position,
		"Email": view.Email, "Phone": view.Phone, "Status": view.Status,
		"ManagerName": view.ManagerName, "HireDate": view.HireDate, "AvatarKey": view.AvatarKey,
	} {
		if got != "" && strings.Contains(got, emp.Remark) {
			t.Errorf("备注漏进了 %s：%q", name, got)
		}
		if got == "2030-12-31" {
			t.Errorf("离职日期漏进了 %s", name)
		}
	}
}

func TestUpdateMyProfileTouchesOnlyThePhoneAndTheEnglishName(t *testing.T) {
	s, ctx := profileTestService(t)
	emp := seedProfileEmployee(t, s, ctx)

	if _, err := s.UpdateMyProfile(ctx, 1, emp.ID, UpdateMyProfileInput{
		EnglishName: "Alice", Phone: "13900000001", ExpectedVersion: emp.Version,
	}); err != nil {
		t.Fatalf("自助保存失败: %v", err)
	}

	after, err := s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: 1, ID: emp.ID})
	if err != nil {
		t.Fatal(err)
	}
	if after.EnglishName != "Alice" || after.Phone != "13900000001" {
		t.Fatalf("该改的没改到：english=%q phone=%q", after.EnglishName, after.Phone)
	}
	// 不该动的一个都不能动。工号和邮箱尤其要紧——邮箱是登录标识。
	for _, c := range []struct{ name, before, now string }{
		{"工号", emp.Code, after.Code},
		{"姓名", emp.Name, after.Name},
		{"邮箱", emp.Email, after.Email},
		{"岗位", emp.Position, after.Position},
		{"备注", emp.Remark, after.Remark},
	} {
		if c.before != c.now {
			t.Errorf("%s 被自助保存改掉了：%q → %q", c.name, c.before, c.now)
		}
	}
	if emp.DepartmentID != after.DepartmentID {
		t.Errorf("部门被自助保存改掉了：%d → %d", emp.DepartmentID, after.DepartmentID)
	}
	if emp.LeaveDate.Valid != after.LeaveDate.Valid || emp.LeaveDate.Time != after.LeaveDate.Time {
		t.Error("离职日期被自助保存改掉了")
	}
}

func TestUpdateMyProfileRefusesAStaleVersion(t *testing.T) {
	s, ctx := profileTestService(t)
	emp := seedProfileEmployee(t, s, ctx)

	if _, err := s.UpdateMyProfile(ctx, 1, emp.ID, UpdateMyProfileInput{
		Phone: "13900000002", ExpectedVersion: emp.Version,
	}); err != nil {
		t.Fatalf("第一次保存就该成功: %v", err)
	}
	// 拿旧版本号再存一次：模拟管理员改过之后员工才按保存。
	_, err := s.UpdateMyProfile(ctx, 1, emp.ID, UpdateMyProfileInput{
		Phone: "13900000003", ExpectedVersion: emp.Version,
	})
	if err == nil {
		t.Fatal("用过期版本号保存应该被拒，却成功了——后写的会盖掉别人的修改")
	}
	if !strings.Contains(err.Error(), "刷新") {
		t.Errorf("报错该告诉人怎么办（刷新重试），实际是: %v", err)
	}
}

// ---------------------------------------------------------------- 头像

// fakeFiles 记下被问过哪些 key，并且可以假装对象是什么大小、什么类型。
type fakeFiles struct {
	statSize    int64
	statType    string
	statErr     error
	removed     []string
	presignedAs string
}

func (f *fakeFiles) PresignPut(_ context.Context, key string) (string, int32, error) {
	f.presignedAs = key
	return "https://storage.test/put/" + key, 600, nil
}
func (f *fakeFiles) PresignGet(_ context.Context, key string) (string, error) {
	return "https://storage.test/get/" + key, nil
}
func (f *fakeFiles) Stat(_ context.Context, _ string) (int64, string, error) {
	if f.statErr != nil {
		return 0, "", f.statErr
	}
	return f.statSize, f.statType, nil
}
func (f *fakeFiles) Remove(_ context.Context, key string) error {
	f.removed = append(f.removed, key)
	return nil
}

// 这是这一组里最要紧的一条。
//
// 头像走的是预签名直传：前端把图片 PUT 到对象存储，然后回调告诉我们 key 是什么。
// 如果我们照单全收，前端就可以回传桶里**任意一个对象**的 key——比如某份合同
// PDF——于是这个人的「头像地址」变成了那份合同的下载地址。
func TestSetAvatarRefusesAKeyThatIsNotThisEmployeesOwn(t *testing.T) {
	s, ctx := profileTestService(t)
	emp := seedProfileEmployee(t, s, ctx)
	files := &fakeFiles{statSize: 1024, statType: "image/png"}
	s.UseFiles(files)

	for _, foreign := range []string{
		"contracts/t1/secret-contract.pdf",                // 别的业务模块的文件
		fmt.Sprintf("avatars/t1/e%d/x.png", emp.ID+1),     // 另一个员工的头像
		"avatars/t2/e1/x.png",                             // 另一家公司
		"../avatars/t1/e" + fmt.Sprint(emp.ID) + "/x.png", // 想用相对路径绕过
	} {
		if _, err := s.SetAvatar(ctx, 1, emp.ID, 1, foreign); err == nil {
			t.Errorf("这个 key 不该被接受，却过了：%q", foreign)
		}
	}

	// 自己的 key 要能过，否则上面那几条可能只是因为全都拒绝而「通过」。
	key, _, _, err := s.PresignAvatarUpload(ctx, 1, emp.ID, "photo.png", "image/png")
	if err != nil {
		t.Fatalf("取上传地址失败: %v", err)
	}
	if _, err := s.SetAvatar(ctx, 1, emp.ID, 1, key); err != nil {
		t.Fatalf("自己的 key 应该被接受: %v", err)
	}
}

func TestSetAvatarTrustsTheStorageNotTheBrowser(t *testing.T) {
	s, ctx := profileTestService(t)
	emp := seedProfileEmployee(t, s, ctx)
	files := &fakeFiles{}
	s.UseFiles(files)
	key, _, _, err := s.PresignAvatarUpload(ctx, 1, emp.ID, "photo.png", "image/png")
	if err != nil {
		t.Fatal(err)
	}

	// 前端声称传的是 PNG，实际落进桶里的是一个 50 MB 的东西。
	files.statSize, files.statType = 50<<20, "image/png"
	if _, err := s.SetAvatar(ctx, 1, emp.ID, 1, key); err == nil {
		t.Error("超大的对象应该被拒——大小要以存储里的为准，不以前端报的为准")
	}

	// 大小正常，但真实类型是可执行文件。
	files.statSize, files.statType = 1024, "application/octet-stream"
	if _, err := s.SetAvatar(ctx, 1, emp.ID, 1, key); err == nil {
		t.Error("非图片应该被拒")
	}

	// 对象压根不在（前端 PUT 失败了却照样回调）。
	files.statSize, files.statType, files.statErr = 1024, "image/png", errors.New("not found")
	if _, err := s.SetAvatar(ctx, 1, emp.ID, 1, key); err == nil {
		t.Error("对象不存在应该被拒")
	}
}

func TestSetAvatarRemovesTheOldPhoto(t *testing.T) {
	s, ctx := profileTestService(t)
	emp := seedProfileEmployee(t, s, ctx)
	files := &fakeFiles{statSize: 1024, statType: "image/jpeg"}
	s.UseFiles(files)

	first, _, _, err := s.PresignAvatarUpload(ctx, 1, emp.ID, "a.jpg", "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetAvatar(ctx, 1, emp.ID, 1, first); err != nil {
		t.Fatal(err)
	}
	second, _, _, err := s.PresignAvatarUpload(ctx, 1, emp.ID, "b.jpg", "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("两次上传应该拿到不同的 key，否则换了照片浏览器还显示旧的")
	}
	if _, err := s.SetAvatar(ctx, 1, emp.ID, 1, second); err != nil {
		t.Fatal(err)
	}
	if len(files.removed) != 1 || files.removed[0] != first {
		t.Errorf("旧图该被删掉，实际删了 %v", files.removed)
	}
}

func TestPresignAvatarRefusesNonImages(t *testing.T) {
	s, ctx := profileTestService(t)
	s.UseFiles(&fakeFiles{})
	for _, ct := range []string{"application/pdf", "text/html", "image/svg+xml", ""} {
		if _, _, _, err := s.PresignAvatarUpload(ctx, 1, 1, "x", ct); err == nil {
			t.Errorf("%q 不该拿到上传地址", ct)
		}
	}
}
