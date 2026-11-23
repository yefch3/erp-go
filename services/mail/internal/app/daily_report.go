package app

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/dailyreport"
)

const DailyReportScope = dailyreport.Scope

type DailyMailCounts struct {
	Received, Sent, Replied, Pending, Overdue int64
	OldestPendingAt                           time.Time
}

type DailyMailEmployee struct {
	EmployeeID int64
	DailyMailCounts
}

type DailyMailReport struct {
	AsOf                       time.Time
	Total                      DailyMailCounts
	Employees                  []DailyMailEmployee
	Trend                      map[string]DailyMailCounts
	Mailboxes, SyncedMailboxes int64
}

func (s *Service) DailyReport(ctx context.Context, tenantID, viewerID int64, day, asOf time.Time, trendUnit string) (DailyMailReport, error) {
	out := DailyMailReport{AsOf: asOf, Trend: map[string]DailyMailCounts{}}
	if tenantID <= 0 || viewerID <= 0 || s.scopes == nil {
		return out, apierr.Permission("MAIL_REPORT_SCOPE_REQUIRED", "无法确认经营报表的数据范围")
	}
	if err := dailyreport.ValidateWindow(day, asOf, trendUnit); err != nil {
		return out, err
	}
	scope, err := s.scopes.VisibleEmployees(ctx, viewerID, DailyReportScope)
	if err != nil {
		return out, err
	}

	// The pending queue is a separate recent snapshot, even in an annual report.
	cutoff := asOf.In(time.FixedZone("UTC+8", 8*3600)).Add(-time.Nanosecond).AddDate(0, 0, -29)
	pendingStart := time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, cutoff.Location())
	rows, err := s.pool.Query(ctx, dailyMailReportSQL, tenantID, scope.All, scope.EmployeeIDs,
		day, asOf, trendUnit, pendingStart)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var counts DailyMailCounts
		var oldest *time.Time
		var bucket string
		if err := rows.Scan(&id, &counts.Received, &counts.Sent, &counts.Replied, &counts.Pending,
			&counts.Overdue, &oldest, &out.Mailboxes, &out.SyncedMailboxes, &bucket); err != nil {
			return out, err
		}
		if oldest != nil {
			counts.OldestPendingAt = *oldest
		}
		if bucket != "" {
			out.Trend[bucket] = counts
		} else if id == 0 {
			out.Total = counts
		} else {
			out.Employees = append(out.Employees, DailyMailEmployee{EmployeeID: id, DailyMailCounts: counts})
		}
	}
	return out, rows.Err()
}

// Inbound Message-IDs deduplicate copies across team members in the team
// total. Each member's row still counts the mail that reached their mailboxes.
// A successful ERP send takes precedence over its imported Sent copy.
// Follow-up uses the latest qualifying inbound message in each mailbox
// conversation and a subsequent actual reply in that same mailbox.
const dailyMailReportSQL = `
WITH boxes AS (
  SELECT a.id, a.employee_id, lower(a.email) AS address, a.unbound_at
  FROM mail_accounts a
  WHERE a.tenant_id=$1 AND a.created_at < $5
    AND (a.unbound_at IS NULL OR a.unbound_at >= least($4::timestamptz,$7::timestamptz))
    AND ($2::bool OR a.employee_id=ANY($3::bigint[]))
), coverage AS (
  SELECT count(*) AS boxes, count(*) FILTER (WHERE EXISTS (
    SELECT 1 FROM mail_sync_state s WHERE s.tenant_id=$1 AND s.account_id=b.id
      AND s.folder='INBOX' AND s.last_synced_at IS NOT NULL
  )) AS synced
  FROM boxes b WHERE b.unbound_at IS NULL OR b.unbound_at >= $5
), inbox AS (
  SELECT i.owner_id AS employee_id, i.account_id, coalesce(i.sent_at,i.received_at) AS at,
    coalesce(nullif(lower(i.message_id),''),'in:'||i.id) AS message_key,
    coalesce(nullif(i.thread_key,''),nullif(i.message_id,''),'in:'||i.id) AS thread_key,
    lower(i.from_email) AS sender
  FROM email_inbound i JOIN boxes b ON b.id=i.account_id AND b.employee_id=i.owner_id
  WHERE i.tenant_id=$1 AND i.folder <> 'SENT' AND (i.folder<>'JUNK' OR i.not_junk)
    AND NOT i.is_bounce AND (i.deleted_at IS NULL OR i.deleted_at >= $5)
    AND lower(i.from_email) <> b.address
    AND coalesce(i.sent_at,i.received_at) >= least($4::timestamptz,$7::timestamptz) AND coalesce(i.sent_at,i.received_at) < $5
), erp_sent AS (
  SELECT m.sender_id AS employee_id, m.account_id, m.sent_at AS at,
    'erp:'||m.id AS message_key, m.thread_key, m.in_reply_to
  FROM email_messages m
  WHERE m.tenant_id=$1 AND m.sender_id>0 AND ($2 OR m.sender_id=ANY($3::bigint[]))
    AND m.sent_at >= least($4::timestamptz,$7::timestamptz) AND m.sent_at < $5
    AND m.status IN ('ACCEPTED','DELIVERED','SOFT_BOUNCED','HARD_BOUNCED','COMPLAINED')
), outbound AS (
  SELECT * FROM erp_sent
  UNION ALL
  SELECT i.owner_id, i.account_id, coalesce(i.sent_at,i.received_at),
    coalesce(nullif(lower(i.message_id),''),'host:'||i.id), i.thread_key, i.in_reply_to
  FROM email_inbound i JOIN boxes b ON b.id=i.account_id AND b.employee_id=i.owner_id
  WHERE i.tenant_id=$1 AND i.folder='SENT' AND NOT i.is_bounce
    AND coalesce(i.sent_at,i.received_at) >= least($4::timestamptz,$7::timestamptz) AND coalesce(i.sent_at,i.received_at) < $5
    AND NOT EXISTS (SELECT 1 FROM email_messages m WHERE m.tenant_id=$1 AND m.id=i.sent_message_id
      AND m.sent_at IS NOT NULL AND m.status IN ('ACCEPTED','DELIVERED','SOFT_BOUNCED','HARD_BOUNCED','COMPLAINED'))
), replies AS (
  SELECT employee_id, account_id, at, coalesce(nullif(thread_key,''),in_reply_to) AS thread_key
  FROM outbound WHERE in_reply_to <> ''
), last_external AS (
  SELECT employee_id, account_id, thread_key, max(at) AS at
  FROM inbox i
  WHERE i.at >= $7 AND EXISTS (
    SELECT 1 FROM boxes b WHERE b.id=i.account_id AND (b.unbound_at IS NULL OR b.unbound_at >= $5)
  ) AND i.sender <> '' AND split_part(i.sender,'@',1) !~ '^(no[._-]?reply|mailer-daemon|postmaster)$'
    AND NOT EXISTS (SELECT 1 FROM mail_accounts a WHERE a.tenant_id=$1 AND lower(a.email)=i.sender)
  GROUP BY employee_id, account_id, thread_key
), pending AS (
  SELECT i.* FROM last_external i WHERE NOT EXISTS (
    SELECT 1 FROM replies r WHERE r.employee_id=i.employee_id AND r.account_id=i.account_id
      AND r.thread_key=i.thread_key AND r.at >= i.at
  )
), events AS (
  SELECT DISTINCT employee_id, 'covered' AS kind, '' AS item, $4::timestamptz AS at FROM boxes
  UNION ALL SELECT employee_id, 'in', message_key, at FROM inbox WHERE at >= $4
  UNION ALL SELECT employee_id, 'out', message_key, at FROM outbound WHERE at >= $4
  UNION ALL SELECT employee_id, 'reply', thread_key, at FROM replies WHERE at >= $4
  UNION ALL SELECT employee_id, 'pending', account_id::text||':'||thread_key, at FROM pending
),
bucketed AS (
  SELECT *, CASE WHEN kind IN ('in','out','reply')
    THEN date_trunc(nullif($6::text,''), at AT TIME ZONE 'Asia/Shanghai') END AS bucket FROM events
), grouped AS (
  SELECT coalesce(employee_id,0)::bigint AS employee_id,
    count(DISTINCT item) FILTER (WHERE kind='in') AS received,
    count(DISTINCT item) FILTER (WHERE kind='out') AS sent,
    count(DISTINCT item) FILTER (WHERE kind='reply') AS replied,
    count(*) FILTER (WHERE kind='pending') AS pending,
    count(*) FILTER (WHERE kind='pending' AND at < $5 - interval '24 hours') AS overdue,
    min(at) FILTER (WHERE kind='pending') AS oldest,
    coalesce(to_char(bucket,'YYYY-MM-DD'),'') AS bucket
  FROM bucketed GROUP BY GROUPING SETS ((), (employee_id), (bucket))
  HAVING GROUPING(bucket)=1 OR bucket IS NOT NULL
)
SELECT g.employee_id,g.received,g.sent,g.replied,g.pending,g.overdue,g.oldest,c.boxes,c.synced,g.bucket
FROM grouped g CROSS JOIN coverage c ORDER BY g.employee_id
`
