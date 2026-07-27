package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/outbox"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/approval/internal/store"
)

// decisionEvent is the payload business services consume. It is
// self-contained on purpose: a consumer never calls back to ask what happened.
type decisionEvent struct {
	InstanceID int64           `json:"instance_id"`
	BizType    string          `json:"biz_type"`
	BizID      int64           `json:"biz_id"`
	BizNo      string          `json:"biz_no"`
	Result     string          `json:"result"`
	ActedBy    int64           `json:"acted_by"`
	Comment    string          `json:"comment"`
	Summary    json.RawMessage `json:"biz_summary,omitempty"`
}

// Act records one approver's decision and moves the instance forward.
//
// The whole state change is one transaction; the event is appended to the
// outbox inside it, so "instance approved" and "ApprovalApproved published"
// can never disagree.
func (s *Service) Act(ctx context.Context, tenantID, actorID, taskID int64, action Action, comment string) (store.ApprovalInstance, []store.ApprovalTask, error) {
	// Read first, outside the transaction, so the role lookup that a next
	// node may need does not happen with locks held. Everything read here is
	// re-checked under lock below.
	task, err := s.q.GetTaskForUpdate(ctx, store.GetTaskForUpdateParams{TenantID: tenantID, ID: taskID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ApprovalInstance{}, nil, apierr.NotFound("AP_TASK_NOT_FOUND", "审批任务不存在")
		}
		return store.ApprovalInstance{}, nil, err
	}
	// Invariant: a task can only be acted on by the person it is assigned to.
	if task.AssigneeID != actorID {
		return store.ApprovalInstance{}, nil, apierr.Permission("AP_NOT_ASSIGNEE", "该审批任务不属于当前用户")
	}
	inst, err := s.q.GetInstance(ctx, store.GetInstanceParams{TenantID: tenantID, ID: task.InstanceID})
	if err != nil {
		return store.ApprovalInstance{}, nil, err
	}

	var nextNode *store.ApprovalNode
	var nextAssignees []int64
	if action == ActionApprove {
		n, err := s.q.GetNode(ctx, store.GetNodeParams{
			TenantID: tenantID, DefinitionID: inst.DefinitionID, Seq: task.NodeSeq + 1,
		})
		switch {
		case err == nil:
			nextNode = &n
			if nextAssignees, err = s.assigneesFor(ctx, n); err != nil {
				return store.ApprovalInstance{}, nil, err
			}
		case errors.Is(err, pgx.ErrNoRows): // last node: approving finishes the flow
		default:
			return store.ApprovalInstance{}, nil, err
		}
	}

	var out store.ApprovalInstance
	var created []store.ApprovalTask
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.GetInstanceForUpdate(ctx, store.GetInstanceForUpdateParams{TenantID: tenantID, ID: inst.ID})
		if err != nil {
			return err
		}
		if locked.Status != statusRunning {
			return apierr.Conflict("AP_INSTANCE_FINISHED", "该审批已结束")
		}
		out = locked

		acted, err := q.ActOnTask(ctx, store.ActOnTaskParams{
			TenantID: tenantID, ID: taskID, Status: taskStatusFor(action), Comment: comment,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Someone else acted on this task between the read and the lock.
				return apierr.Conflict("AP_TASK_ALREADY_ACTED", "该任务已被处理")
			}
			return err
		}

		if action != ActionApprove {
			result := statusRejected
			if action == ActionReturn {
				result = statusReturned
			}
			return s.finish(ctx, q, tx, tenantID, locked, result, actorID, comment, &out)
		}

		// ALL mode waits for every assignee at this node; ANY closes the rest.
		node, err := q.GetNode(ctx, store.GetNodeParams{
			TenantID: tenantID, DefinitionID: locked.DefinitionID, Seq: acted.NodeSeq,
		})
		if err != nil {
			return err
		}
		if node.ApproveMode == "ALL" {
			pending, err := q.CountPendingAtNode(ctx, store.CountPendingAtNodeParams{
				TenantID: tenantID, InstanceID: locked.ID, NodeSeq: acted.NodeSeq,
			})
			if err != nil {
				return err
			}
			if pending > 0 {
				return nil // still waiting on colleagues; instance unchanged
			}
		} else if err := q.CloseSiblingTasks(ctx, store.CloseSiblingTasksParams{
			TenantID: tenantID, InstanceID: locked.ID, NodeSeq: acted.NodeSeq, Status: taskSkipped,
		}); err != nil {
			return err
		}

		if nextNode == nil {
			return s.finish(ctx, q, tx, tenantID, locked, statusApproved, actorID, comment, &out)
		}
		// Guard against the definition being edited between the two reads.
		fresh, err := q.GetNode(ctx, store.GetNodeParams{
			TenantID: tenantID, DefinitionID: locked.DefinitionID, Seq: nextNode.Seq,
		})
		if err != nil || fresh.ID != nextNode.ID {
			return apierr.Conflict("AP_FLOW_CHANGED", "审批流已变更，请重试")
		}
		if out, err = q.AdvanceInstance(ctx, store.AdvanceInstanceParams{
			TenantID: tenantID, ID: locked.ID, CurrentSeq: nextNode.Seq,
		}); err != nil {
			return err
		}
		created, err = createTasks(ctx, q, tenantID, locked.ID, *nextNode, nextAssignees)
		return err
	})
	if err != nil {
		return store.ApprovalInstance{}, nil, err
	}
	return out, created, nil
}

// finish closes the instance, cancels what is still pending and appends the
// decision event in the same transaction.
func (s *Service) finish(ctx context.Context, q *store.Queries, tx pgx.Tx, tenantID int64, inst store.ApprovalInstance, result string, actorID int64, comment string, out *store.ApprovalInstance) error {
	if err := q.CancelInstanceTasks(ctx, store.CancelInstanceTasksParams{
		TenantID: tenantID, InstanceID: inst.ID,
	}); err != nil {
		return err
	}
	finished, err := q.FinishInstance(ctx, store.FinishInstanceParams{
		TenantID: tenantID, ID: inst.ID, Status: result,
	})
	if err != nil {
		return err
	}
	*out = finished

	payload, err := json.Marshal(decisionEvent{
		InstanceID: inst.ID, BizType: inst.BizType, BizID: inst.BizID, BizNo: inst.BizNo,
		Result: result, ActedBy: actorID, Comment: comment, Summary: inst.BizSummary,
	})
	if err != nil {
		return err
	}
	return outbox.Append(ctx, tx, outbox.Event{
		TenantID: tenantID, AggregateType: "approval",
		AggregateID: instanceKey(inst.ID), EventType: eventTypeFor(result),
		Payload: payload,
	})
}

func taskStatusFor(a Action) string {
	switch a {
	case ActionReject:
		return statusRejected
	case ActionReturn:
		return statusReturned
	default:
		return statusApproved
	}
}

func eventTypeFor(result string) string {
	switch result {
	case statusRejected:
		return "ApprovalRejected"
	case statusReturned:
		return "ApprovalReturned"
	default:
		return "ApprovalApproved"
	}
}
