package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

// 员工邮箱监管的入口。老板端按「国家 → 员工 → 收件箱 / 已发送」看员工的
// 来往邮件。
//
// 这几条路**不要 requireMailUnlock**，这是有意的，也是这一组和别的邮件接口
// 最大的不同：信箱令牌是"我知道这个箱的密码"，而看的人恰恰不知道、也不该
// 知道。这里认的是权限（mail:supervision:read）。相应地，邮件服务那边每读
// 一次都会往登记簿写一行，写不进去就不给读——规矩写在
// services/mail/internal/app/supervision.go 的文件头。
//
// 树在这一层拼，不在邮件服务里拼：姓名和国家长在员工身上（IAM），而信箱在
// 邮件服务。两份数据分属两个服务，谁都不该为了画一棵树去读另一个服务的库。

// supervisionEmployee 是树上的一个人。
type supervisionEmployee struct {
	EmployeeID string `json:"employeeId"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	Mailboxes  int64  `json:"mailboxes"`
	Unread     int64  `json:"unread"`
}

// supervisionCountry 是树上的一个国家。
type supervisionCountry struct {
	// 两位国家码，空串表示「未分配国家」——那一档由前端自己取名字。
	CountryCode string                `json:"countryCode"`
	Employees   []supervisionEmployee `json:"employees"`
}

func (s *Server) mailSupervisionTree(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// 先问"有什么可看的"：没绑过信箱的人不该出现在树上——点进去是一片空，
	// 那不是信息，是噪音。
	boxes, err := s.Emails.ListSupervisableEmployees(ctx, &mailv1.ListSupervisableEmployeesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	withMailbox := map[int64]*supervisionEmployee{}
	for _, b := range boxes.GetEmployees() {
		withMailbox[b.GetEmployeeId()] = &supervisionEmployee{
			EmployeeID: strconv.FormatInt(b.GetEmployeeId(), 10),
			Mailboxes:  b.GetMailboxes(),
			Unread:     b.GetUnread(),
		}
	}
	if len(withMailbox) == 0 {
		writeCountries(w, []supervisionCountry{})
		return
	}

	// 名字、工号和国家都从 IAM 来。**不用客户负责人表里那份姓名快照**：那是
	// 签约那天存下的，改过名的人会在树上顶着旧名字，而树是给人看的。
	// 存三个字段，不存整个 proto 消息：proto 结构体里有锁，按值拷进 map 会被
	// go vet 拦下，而这里要的本来就只有这三样。
	type who struct{ name, code, country string }
	names := map[int64]who{}
	emps, err := s.Directory.ListEmployees(ctx, &iamv1.ListEmployeesRequest{
		Page: &commonv1.PageRequest{Page: 1, PageSize: 500},
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	for _, e := range emps.GetEmployees() {
		names[e.GetId()] = who{name: e.GetName(), code: e.GetCode(), country: e.GetCountryCode()}
	}

	// 国家是员工资料上的一条（2026-09-18 起）：他挂在哪个国家，就在哪个国家
	// 下面，一个人只出现一次。原来按他名下客户所在国家推算——一个人名下客户
	// 分布在几个国家就出现几次，没分到客户的人只能进「未分配」；老板要的是
	// 员工自己的国家。没填的进「未分配国家」：他照样有信箱，照样可能是要看的
	// 那个。
	byCountry := map[string]map[int64]bool{}
	for id, row := range withMailbox {
		code := ""
		if e, ok := names[id]; ok {
			row.Name, row.Code = e.name, e.code
			code = e.country
		}
		if byCountry[code] == nil {
			byCountry[code] = map[int64]bool{}
		}
		byCountry[code][id] = true
	}

	out := make([]supervisionCountry, 0, len(byCountry))
	for code, ids := range byCountry {
		c := supervisionCountry{CountryCode: code}
		for id := range ids {
			c.Employees = append(c.Employees, *withMailbox[id])
		}
		sort.Slice(c.Employees, func(a, b int) bool {
			if c.Employees[a].Name != c.Employees[b].Name {
				return c.Employees[a].Name < c.Employees[b].Name
			}
			return c.Employees[a].EmployeeID < c.Employees[b].EmployeeID
		})
		out = append(out, c)
	}
	// 国家码排序，「未分配」永远垫底：它是个兜底的筐，不该排在真国家前面。
	sort.Slice(out, func(a, b int) bool {
		if (out[a].CountryCode == "") != (out[b].CountryCode == "") {
			return out[b].CountryCode == ""
		}
		return out[a].CountryCode < out[b].CountryCode
	})
	writeCountries(w, out)
}

func writeCountries(w http.ResponseWriter, out []supervisionCountry) {
	w.Header().Set("Content-Type", "application/json")
	data, _ := json.Marshal(map[string]any{"countries": out})
	_ = json.NewEncoder(w).Encode(envelope{Success: true, Data: data})
}

// supervisionTarget 读出「要看谁」，并顺带把名字取出来落进日志。
//
// 名字**由网关查**，不收调用方传的：日志里那个名字是用来日后追责的，
// 一个能被调用方写的字段在那件事上不作数。
func (s *Server) supervisionTarget(r *http.Request) (int64, string) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("employee_id"), 10, 64)
	if id <= 0 {
		return 0, ""
	}
	emp, err := s.Directory.GetEmployee(r.Context(), &iamv1.GetEmployeeRequest{Id: id})
	if err != nil {
		return id, ""
	}
	return id, emp.GetEmployee().GetName()
}

func (s *Server) mailSupervisionFolder(w http.ResponseWriter, r *http.Request) {
	target, name := s.supervisionTarget(r)
	op, _ := grpcx.OperatorFromContext(r.Context())
	size := pageFromQuery(r).GetPageSize()
	resp, err := s.Emails.SuperviseFolder(r.Context(), &mailv1.SuperviseFolderRequest{
		EmployeeId: target, EmployeeName: name,
		Folder:   r.URL.Query().Get("folder"),
		Keyword:  r.URL.Query().Get("keyword"),
		Cursor:   r.URL.Query().Get("cursor"),
		PageSize: size,
		// 看的人是谁，从登录令牌来，不从请求参数来。
		ViewerId: op.EmployeeID, ViewerName: op.Name, ClientIp: clientIP(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) mailSupervisionMail(w http.ResponseWriter, r *http.Request) {
	target, name := s.supervisionTarget(r)
	op, _ := grpcx.OperatorFromContext(r.Context())
	resp, err := s.Emails.SuperviseMail(r.Context(), &mailv1.SuperviseMailRequest{
		EmployeeId: target, EmployeeName: name, Id: idFromPath(r),
		ViewerId: op.EmployeeID, ViewerName: op.Name, ClientIp: clientIP(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) mailSupervisionLog(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("employee_id"), 10, 64)
	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 32)
	resp, err := s.Emails.ListSupervisionLog(r.Context(), &mailv1.ListSupervisionLogRequest{
		EmployeeId: id, Limit: int32(limit),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
