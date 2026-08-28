package httpapi

import (
	"net/http"
	"strconv"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
)

// 「我的资料」和头像。
//
// 这一组分成两半，门不一样：
//   - /api/me/* 只要求登录。作用对象永远是调用者自己——iam 那边的
//     GetMyProfile / UpdateMyProfile 连「员工 id」这个入参都没有，
//     所以「改别人的」是个表达不出来的请求，不是被拒绝的请求。
//   - /api/employees/{id}/avatar* 要 iam:employee:write。管理员替别人换照片
//     和改别人的资料是同一件事，就走同一道门。
//
// 头像走预签名直传：浏览器把图片 PUT 到对象存储，不经过网关。省掉一次
// 转发，也省掉网关为图片开一块内存。传完再回调 SetAvatar 把 key 落库——
// 而那一步会重新校验 key 的归属和对象的真实大小类型，因为「前端说传好了」
// 不是证据。

func (s *Server) myProfile(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Directory.GetMyProfile(r.Context(), &iamv1.GetMyProfileRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateMyProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EnglishName     string `json:"englishName"`
		Phone           string `json:"phone"`
		ExpectedVersion int32  `json:"expectedVersion"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	resp, err := s.Directory.UpdateMyProfile(r.Context(), &iamv1.UpdateMyProfileRequest{
		EnglishName:     body.EnglishName,
		Phone:           body.Phone,
		ExpectedVersion: body.ExpectedVersion,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) presignMyAvatar(w http.ResponseWriter, r *http.Request) {
	s.presignAvatar(w, r, 0)
}

func (s *Server) presignEmployeeAvatar(w http.ResponseWriter, r *http.Request) {
	s.presignAvatar(w, r, idFromPath(r))
}

// presignAvatar：employeeID 为 0 时 iam 解释成「调用者自己」。
// 两条路由共用一个实现，区别只在这个 id 从哪来——一个来自路径（管理员指定），
// 一个压根不存在（本人）。
func (s *Server) presignAvatar(w http.ResponseWriter, r *http.Request, employeeID int64) {
	var body struct {
		FileName    string `json:"fileName"`
		ContentType string `json:"contentType"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	resp, err := s.Directory.PresignAvatarUpload(r.Context(), &iamv1.PresignAvatarUploadRequest{
		EmployeeId: employeeID, FileName: body.FileName, ContentType: body.ContentType,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) setMyAvatar(w http.ResponseWriter, r *http.Request) {
	s.setAvatar(w, r, 0)
}

func (s *Server) setEmployeeAvatar(w http.ResponseWriter, r *http.Request) {
	s.setAvatar(w, r, idFromPath(r))
}

func (s *Server) setAvatar(w http.ResponseWriter, r *http.Request, employeeID int64) {
	var body struct {
		// 空字符串表示清除头像，这是有意的：删照片和换照片走同一条路，
		// 不为「删」单开一个 DELETE。
		FileKey string `json:"fileKey"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	resp, err := s.Directory.SetAvatar(r.Context(), &iamv1.SetAvatarRequest{
		EmployeeId: employeeID, FileKey: body.FileKey,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// employeeAvatarURLs 批量换取头像地址。
//
// POST 而不是 GET，因为要传一串 id：几十个 id 塞进查询串会撞上代理和浏览器
// 的 URL 长度上限，而那种失败是间歇性的、只在人多的公司出现。
const avatarURLBatchMax = 500

func (s *Server) employeeAvatarURLs(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EmployeeIDs []string `json:"employeeIds"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if len(body.EmployeeIDs) > avatarURLBatchMax {
		s.writeError(w, http.StatusBadRequest, "IAM_AVATAR_BATCH_TOO_LARGE",
			"一次最多查询 500 个头像")
		return
	}
	ids := make([]int64, 0, len(body.EmployeeIDs))
	for _, raw := range body.EmployeeIDs {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	resp, err := s.Directory.AvatarURLs(r.Context(), &iamv1.AvatarURLsRequest{EmployeeIds: ids})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// orgChart 一次返回画两棵树要的全部数据：人和部门。
//
// 两棵树共用同一批人，分两次请求就是把三百个人传两遍；而前端切换视角是
// 一次点击，不该再等一次网络。
func (s *Server) orgChart(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Directory.GetOrgChart(r.Context(), &iamv1.GetOrgChartRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
