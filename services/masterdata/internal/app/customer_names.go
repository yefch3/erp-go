package app

import (
	"context"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// customerNamesMax bounds one batch. A list page is at most 100 rows; the
// headroom is for callers that page differently, not for "everything".
const customerNamesMax = 500

// CustomerName is what a list needs to show a customer: the name it goes by
// now and whether it is still in use.
type CustomerName struct {
	ID        int64
	Name      string
	ShortName string
	Status    string
}

// CustomerNames reads the current names of a batch of customers in one query.
//
// Visibility is the same rule AuthorizeCustomer applies one id at a time:
// zero access employee sees the tenant, otherwise only customers this person
// currently owns. Ids the caller may not see are simply absent, exactly as
// GetCustomer answers "not found" for them, so a caller cannot use the batch
// to learn more than the single lookup would tell it.
func (s *Service) CustomerNames(ctx context.Context, tenantID int64, ids []int64) ([]CustomerName, error) {
	if len(ids) > customerNamesMax {
		return nil, apierr.Invalid("MD_CUSTOMER_NAMES_TOO_MANY", "一次最多查询 500 个客户")
	}
	wanted := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			wanted = append(wanted, id)
		}
	}
	if len(wanted) == 0 {
		return []CustomerName{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.name, c.short_name, c.status FROM customers c
		WHERE c.tenant_id=$1 AND c.id=ANY($2::bigint[])
		AND ($3::bigint=0 OR EXISTS (
			SELECT 1 FROM customer_owners o WHERE o.tenant_id=c.tenant_id AND o.customer_id=c.id
			AND o.employee_id=$3 AND o.status='ACTIVE'
			AND (o.start_date IS NULL OR o.start_date <= CURRENT_DATE)
			AND (o.end_date IS NULL OR o.end_date >= CURRENT_DATE)))`,
		tenantID, wanted, customerAccessEmployee(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CustomerName, 0, len(wanted))
	for rows.Next() {
		var c CustomerName
		if err := rows.Scan(&c.ID, &c.Name, &c.ShortName, &c.Status); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
