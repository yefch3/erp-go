// Package app holds the approval engine: it turns a flow definition plus one
// business document into tasks, and one approver's decision into the next
// tasks or a finished instance. It knows nothing about what it approves.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/approval/internal/store"
)

// Directory resolves a role into the employees who hold it. Implemented by
// the iam gRPC client; the engine never reads iam's tables.
type Directory interface {
	RoleMembers(ctx context.Context, roleID int64) ([]int64, error)
	// ManagersOf walks the submitter's reporting line: level 1 is their
	// direct manager, 2 is that person's manager. Empty once it runs out.
	ManagersOf(ctx context.Context, employeeID int64, levels int32) ([]int64, error)
}

// Live nudges whoever has a page open. It is a hint, never a guarantee: the
// engine's correctness does not depend on any of these arriving.
type Live interface {
	ToEmployees(ctx context.Context, tenantID int64, employeeIDs []int64, e livefeed.Event)
}

type Service struct {
	pool                        *pgxpool.Pool
	q                           *store.Queries
	dir                         Directory
	live                        Live
	purchaseOrderFallbackRoleID int64
}

func New(pool *pgxpool.Pool, dir Directory, live Live, purchaseOrderFallbackRoleID int64) *Service {
	return &Service{
		pool: pool, q: store.New(pool), dir: dir, live: live,
		purchaseOrderFallbackRoleID: purchaseOrderFallbackRoleID,
	}
}

// nudge tells a set of employees that something they are looking at moved.
//
// The event type matters: an approver's QUEUE changed, while the submitter's
// DOCUMENT changed. They are different pages watching for different things,
// and collapsing both into one type would make the submitter's contract page
// reload every time an unrelated task landed in their todo list.
//
// Always called after the transaction commits: a hint about a change that got
// rolled back would send the browser to read something that never happened.
func (s *Service) nudge(ctx context.Context, tenantID int64, employeeIDs []int64, eventType, subject string) {
	if s.live == nil || len(employeeIDs) == 0 {
		return
	}
	s.live.ToEmployees(ctx, tenantID, employeeIDs, livefeed.Event{
		Type: eventType, Subject: subject,
	})
}

// Action is what an approver did with a task.
type Action string

const (
	ActionApprove Action = "APPROVE"
	ActionReject  Action = "REJECT"
	ActionReturn  Action = "RETURN"
)

const (
	statusRunning  = "RUNNING"
	statusApproved = "APPROVED"
	statusRejected = "REJECTED"
	statusReturned = "RETURNED"
	taskPending    = "PENDING"
	taskSkipped    = "SKIPPED"
)

// ---------------------------------------------------------------- submit

type SubmitInput struct {
	BizType       string
	BizID         int64
	BizNo         string
	BizSummary    string // JSON object; "" becomes {}
	SubmitterID   int64
	SubmitterName string
	// What the flow is chosen by; the engine only compares it against band
	// floors and never interprets it. Empty means zero.
	Amount string
}

func (in SubmitInput) validate() error {
	switch {
	case in.BizType == "":
		return apierr.Invalid("AP_BIZ_TYPE_REQUIRED", "单据类型必填")
	case in.BizID == 0:
		return apierr.Invalid("AP_BIZ_ID_REQUIRED", "单据 ID 必填")
	case in.SubmitterID == 0:
		return apierr.Invalid("AP_SUBMITTER_REQUIRED", "提交人必填")
	}
	if in.BizSummary != "" && !json.Valid([]byte(in.BizSummary)) {
		return apierr.Invalid("AP_SUMMARY_INVALID", "单据摘要必须是合法 JSON")
	}
	return nil
}

func (s *Service) Submit(ctx context.Context, tenantID int64, in SubmitInput) (store.ApprovalInstance, []store.ApprovalTask, error) {
	if err := in.validate(); err != nil {
		return store.ApprovalInstance{}, nil, err
	}
	amount, err := normalizeAmount(in.Amount)
	if err != nil {
		return store.ApprovalInstance{}, nil, err
	}
	def, err := s.q.ActiveDefinitionFor(ctx, store.ActiveDefinitionForParams{
		TenantID: tenantID, BizType: in.BizType, Amount: amount,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ApprovalInstance{}, nil, apierr.NotFound("AP_DEFINITION_NOT_FOUND", "该单据类型未配置审批流").
				WithMeta("biz_type", in.BizType)
		}
		return store.ApprovalInstance{}, nil, err
	}
	nodes, err := s.q.ListNodes(ctx, store.ListNodesParams{TenantID: tenantID, DefinitionID: def.ID})
	if err != nil {
		return store.ApprovalInstance{}, nil, err
	}
	if len(nodes) == 0 {
		return store.ApprovalInstance{}, nil, apierr.Invalid("AP_DEFINITION_EMPTY", "审批流没有配置任何节点")
	}
	// Approvers are resolved over gRPC, so it happens before the transaction
	// opens: a database transaction must never wait on another service.
	firstNode, assignees, err := s.firstStaffedNode(ctx, nodes, 0, in.SubmitterID)
	if err != nil {
		return store.ApprovalInstance{}, nil, err
	}
	// 采购单会形成真实的付款承诺。组织架构没有上级时，转交配置的
	// 采购审批角色，仍然生成待办，不能自动通过或停在草稿之外。
	if firstNode == nil && usesPurchaseOrderFallback(in.BizType) {
		fallback, err := s.dir.RoleMembers(ctx, s.purchaseOrderFallbackRoleID)
		if err != nil {
			return store.ApprovalInstance{}, nil, fmt.Errorf("approval: resolve purchase fallback role %d: %w", s.purchaseOrderFallbackRoleID, err)
		}
		fallback = preferOtherApprovers(fallback, in.SubmitterID)
		if len(fallback) == 0 {
			return store.ApprovalInstance{}, nil, apierr.Invalid(
				"AP_APPROVER_REQUIRED", "采购单没有可用审批人，请先配置采购审批角色成员",
			)
		}
		node := nodes[0]
		node.Name = "采购审批人审批"
		firstNode, assignees = &node, fallback
	}

	summary := in.BizSummary
	if summary == "" {
		summary = "{}"
	}
	var inst store.ApprovalInstance
	var tasks []store.ApprovalTask
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		inst, err = q.CreateInstance(ctx, store.CreateInstanceParams{
			TenantID: tenantID, DefinitionID: def.ID, BizType: in.BizType, BizID: in.BizID,
			BizNo: in.BizNo, BizSummary: []byte(summary),
			SubmitterID: in.SubmitterID, SubmitterName: in.SubmitterName,
			Amount: amount,
		})
		if err != nil {
			var pgErr interface{ SQLState() string }
			if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
				return apierr.Conflict("AP_ALREADY_RUNNING", "该单据已有审批在进行中")
			}
			return err
		}
		if firstNode == nil {
			// Nobody above the submitter to ask. The instance is recorded
			// anyway so the document still has an audit trail saying exactly
			// that, and the business service still gets its event.
			_, err := s.finish(ctx, q, tx, tenantID, inst, statusApproved, in.SubmitterID,
				"提交人之上没有审批人，自动通过", &inst)
			return err
		}
		tasks, err = createTasks(ctx, q, tenantID, inst.ID, *firstNode, assignees)
		return err
	})
	if err != nil {
		return store.ApprovalInstance{}, nil, err
	}
	s.nudge(ctx, tenantID, assigneesOf(tasks), livefeed.TodoChanged, subjectOf(inst))
	return inst, tasks, nil
}

// usesPurchaseOrderFallback 标记必须在无直属上级时转交审批角色的单据。
func usesPurchaseOrderFallback(bizType string) bool {
	return bizType == "PURCHASE_ORDER"
}

// preferOtherApprovers 有其他审批人时排除提交人；只有提交人一人时仍保留
// 人工待办，保证开发或应急账号也不会被系统自动批准。
func preferOtherApprovers(ids []int64, submitterID int64) []int64 {
	others := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != submitterID {
			others = append(others, id)
		}
	}
	if len(others) > 0 {
		return others
	}
	return ids
}

func assigneesOf(tasks []store.ApprovalTask) []int64 {
	ids := make([]int64, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.AssigneeID)
	}
	return ids
}

func subjectOf(inst store.ApprovalInstance) string {
	return fmt.Sprintf("%s:%d", inst.BizType, inst.BizID)
}

// assigneesFor turns a node into the employees who must act on it.
//
// submitterID is needed because a node can be relative to whoever sent the
// document in ("my manager") rather than absolute ("the sales manager role").
func (s *Service) assigneesFor(ctx context.Context, node store.ApprovalNode, submitterID int64) ([]int64, error) {
	var ids []int64
	switch node.ApproverType {
	case "EMPLOYEE":
		ids = []int64{node.ApproverRef}
	case "ROLE":
		members, err := s.dir.RoleMembers(ctx, node.ApproverRef)
		if err != nil {
			return nil, fmt.Errorf("approval: resolve role %d: %w", node.ApproverRef, err)
		}
		ids = members
	case "MANAGER":
		// Always measured from the SUBMITTER, with approver_ref saying how far
		// up: node 1 is their manager, node 2 that person's manager. Chaining
		// from whoever approved last would instead make the number of levels
		// depend on where in the org chart a document happened to start.
		//
		// An empty answer is not an error here: the chain has run out, and
		// the caller decides whether to skip the node or finish the flow.
		managers, err := s.dir.ManagersOf(ctx, submitterID, managerLevels(node))
		if err != nil {
			return nil, fmt.Errorf("approval: resolve manager of %d: %w", submitterID, err)
		}
		return managers, nil
	default:
		return nil, apierr.Invalid("AP_APPROVER_TYPE_INVALID", "不支持的审批人类型").
			WithMeta("approver_type", node.ApproverType)
	}
	if len(ids) == 0 {
		// A role with nobody in it is a misconfiguration, unlike a reporting
		// line that has simply reached the top.
		return nil, apierr.Invalid("AP_NO_APPROVER", "审批节点没有可用审批人").
			WithMeta("node", node.Name)
	}
	return ids, nil
}

// managerLevels reads approver_ref as "how far up the reporting line". Zero
// means the direct manager, so an unset column behaves as one level.
func managerLevels(node store.ApprovalNode) int32 {
	if node.ApproverRef < 1 {
		return 1
	}
	return int32(node.ApproverRef)
}

// firstStaffedNode walks forward from a node index looking for one that has
// somebody to assign. Manager nodes go unstaffed at the top of the reporting
// line, and a flow of two levels submitted by the person at the top has
// nobody left to ask - skipping is the only sensible reading of that.
//
// Returns a nil node when every remaining node is empty, which means the
// document is approved by default.
func (s *Service) firstStaffedNode(ctx context.Context, nodes []store.ApprovalNode, from int, submitterID int64) (*store.ApprovalNode, []int64, error) {
	for i := from; i < len(nodes); i++ {
		assignees, err := s.assigneesFor(ctx, nodes[i], submitterID)
		if err != nil {
			return nil, nil, err
		}
		if len(assignees) > 0 {
			node := nodes[i]
			return &node, assignees, nil
		}
	}
	return nil, nil, nil
}

func createTasks(ctx context.Context, q *store.Queries, tenantID, instanceID int64, node store.ApprovalNode, assignees []int64) ([]store.ApprovalTask, error) {
	tasks := make([]store.ApprovalTask, 0, len(assignees))
	for _, id := range assignees {
		t, err := q.CreateTask(ctx, store.CreateTaskParams{
			TenantID: tenantID, InstanceID: instanceID, NodeSeq: node.Seq,
			NodeName: node.Name, AssigneeID: id,
		})
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}
