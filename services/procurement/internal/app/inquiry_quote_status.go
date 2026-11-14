package app

import "fmt"

// Department progress belongs to the viewer, not to the whole department.
// Only submitted, live quotes from the current inquiry revision participate.
// Opening a page is not an acknowledgement: another writer's later submission
// remains UPDATED until the viewer next saves/submits their own live quote.
func inquiryPersonalQuoteStatus(alias, kind, operator string) string {
	if kind != "PROCUREMENT" && kind != "LOGISTICS" {
		return fmt.Sprintf(`CASE WHEN %s::bigint>0 THEN '' ELSE '' END`, operator)
	}
	return fmt.Sprintf(`(SELECT CASE
		WHEN count(*) FILTER (WHERE q.created_by=%[3]s)=0 THEN 'WAITING'
		WHEN max(GREATEST(q.updated_at,q.submitted_at)) FILTER (WHERE q.created_by<>%[3]s OR q.updated_by<>%[3]s) >
			max(CASE WHEN q.created_by=%[3]s THEN CASE WHEN q.updated_by=%[3]s THEN GREATEST(q.updated_at,q.submitted_at) ELSE q.submitted_at END END)
		THEN 'UPDATED' ELSE 'MY_QUOTED' END
		FROM inquiry_quotes q WHERE q.tenant_id=%[1]s.tenant_id AND q.case_id=%[1]s.id
		AND q.inquiry_revision=%[1]s.inquiry_revision AND q.kind='%[2]s'
		AND q.deleted_at IS NULL AND q.submitted_at IS NOT NULL)`, alias, kind, operator)
}
