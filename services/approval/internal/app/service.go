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
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/approval/internal/store"
)

// Directory resolves a role into the employees who hold it. Implemented by
// the iam gRPC client; the engine never reads iam's tables.
type Directory interface {
	RoleMembers(ctx context.Context, roleID int64) ([]int64, error)
}

type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
	dir  Directory
}

func New(pool *pgxpool.Pool, dir Directory) *Service {
	return &Service{pool: pool, q: store.New(pool), dir: dir}
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
	def, err := s.q.ActiveDefinitionFor(ctx, store.ActiveDefinitionForParams{TenantID: tenantID, BizType: in.BizType})
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
	assignees, err := s.assigneesFor(ctx, nodes[0])
	if err != nil {
		return store.ApprovalInstance{}, nil, err
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
		})
		if err != nil {
			var pgErr interface{ SQLState() string }
			if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
				return apierr.Conflict("AP_ALREADY_RUNNING", "该单据已有审批在进行中")
			}
			return err
		}
		tasks, err = createTasks(ctx, q, tenantID, inst.ID, nodes[0], assignees)
		return err
	})
	if err != nil {
		return store.ApprovalInstance{}, nil, err
	}
	return inst, tasks, nil
}

// assigneesFor turns a node into the employees who must act on it.
func (s *Service) assigneesFor(ctx context.Context, node store.ApprovalNode) ([]int64, error) {
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
	default:
		return nil, apierr.Invalid("AP_APPROVER_TYPE_INVALID", "不支持的审批人类型").
			WithMeta("approver_type", node.ApproverType)
	}
	if len(ids) == 0 {
		// Better to refuse than to create an instance nobody can act on.
		return nil, apierr.Invalid("AP_NO_APPROVER", "审批节点没有可用审批人").
			WithMeta("node", node.Name)
	}
	return ids, nil
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
