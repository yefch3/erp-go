package httpapi

import (
	"encoding/csv"
	"net/http"
	"net/url"
	"sort"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// 询盘列模板的管理门面。业务规则（版本化、核心字段、唯一默认）全部在
// procurement 服务里，这里只做 HTTP↔gRPC 翻译。

func (s *Server) listInquiryTemplates(w http.ResponseWriter, r *http.Request) {
	resp, err := s.InquiryTemplates.ListInquiryTemplates(r.Context(), &prv1.ListInquiryTemplatesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getInquiryTemplate(w http.ResponseWriter, r *http.Request) {
	resp, err := s.InquiryTemplates.GetInquiryTemplate(r.Context(), &prv1.GetInquiryTemplateRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createInquiryTemplate(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateInquiryTemplateRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.InquiryTemplates.CreateInquiryTemplate(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) saveInquiryTemplate(w http.ResponseWriter, r *http.Request) {
	req := &prv1.SaveInquiryTemplateRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.InquiryTemplates.SaveInquiryTemplate(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) setInquiryTemplateStatus(w http.ResponseWriter, r *http.Request) {
	req := &prv1.SetInquiryTemplateStatusRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.InquiryTemplates.SetInquiryTemplateStatus(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) setDefaultInquiryTemplate(w http.ResponseWriter, r *http.Request) {
	resp, err := s.InquiryTemplates.SetDefaultInquiryTemplate(r.Context(), &prv1.SetDefaultInquiryTemplateRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// 下载模板 = 按字段顺序输出表头行的 CSV。Excel 打开中文 CSV 需要 BOM。
func (s *Server) downloadInquiryTemplate(w http.ResponseWriter, r *http.Request) {
	resp, err := s.InquiryTemplates.GetInquiryTemplate(r.Context(), &prv1.GetInquiryTemplateRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	fields := resp.GetTemplate().GetFields()
	sort.SliceStable(fields, func(i, j int) bool {
		if fields[i].GetSortOrder() != fields[j].GetSortOrder() {
			return fields[i].GetSortOrder() < fields[j].GetSortOrder()
		}
		return fields[i].GetFieldKey() < fields[j].GetFieldKey()
	})
	headers := make([]string, 0, len(fields))
	for _, field := range fields {
		headers = append(headers, field.GetDisplayName())
	}
	fileName := resp.GetTemplate().GetTemplateCode() + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(fileName))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("\xef\xbb\xbf")); err != nil {
		return
	}
	writer := csv.NewWriter(w)
	_ = writer.Write(headers)
	writer.Flush()
}
