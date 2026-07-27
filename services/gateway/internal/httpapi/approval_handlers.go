package httpapi

import (
	"net/http"
	"strconv"

	apv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
)

func (s *Server) myTodos(w http.ResponseWriter, r *http.Request) {
	// No assignee parameter on purpose: approval derives it from the caller's
	// identity, so nobody can read another employee's queue.
	resp, err := s.Approval.MyTodos(r.Context(), &apv1.MyTodosRequest{
		Page:    pageFromQuery(r),
		BizType: r.URL.Query().Get("biz_type"),
		// "" is the pending queue; HANDLED / APPROVED / REJECTED / RETURNED
		// are the "what did I decide" tabs.
		Status: r.URL.Query().Get("status"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// actBody is the JSON shape of a decision; action is a word, not a number,
// so the API stays readable without the proto enum.
type actBody struct {
	Action  string `json:"action"`
	Comment string `json:"comment"`
}

func (s *Server) actOnTask(w http.ResponseWriter, r *http.Request) {
	var body actBody
	if !s.decodeJSON(w, r, &body) {
		return
	}
	action, ok := map[string]apv1.Action{
		"APPROVE": apv1.Action_ACTION_APPROVE,
		"REJECT":  apv1.Action_ACTION_REJECT,
		"RETURN":  apv1.Action_ACTION_RETURN,
	}[body.Action]
	if !ok {
		s.writeError(w, http.StatusBadRequest, "AP_ACTION_INVALID",
			"审批动作必须是 APPROVE / REJECT / RETURN")
		return
	}
	resp, err := s.Approval.Act(r.Context(), &apv1.ActRequest{
		TaskId: idFromPath(r), Action: action, Comment: body.Comment,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listApprovalInstances(w http.ResponseWriter, r *http.Request) {
	bizID, _ := strconv.ParseInt(r.URL.Query().Get("biz_id"), 10, 64)
	resp, err := s.Approval.ListInstances(r.Context(), &apv1.ListInstancesRequest{
		BizType: r.URL.Query().Get("biz_type"), BizId: bizID,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getApprovalInstance(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Approval.GetInstance(r.Context(), &apv1.GetInstanceRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
