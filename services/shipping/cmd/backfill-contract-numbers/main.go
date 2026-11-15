// backfill-contract-numbers repairs metadata only. It never calls upload,
// verification or payment APIs, whose side effects are inappropriate here.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type audit struct {
	HandoffID       int64           `json:"handoffId"`
	ContractNo      string          `json:"contractNo"`
	PayableID       int64           `json:"payableId,omitempty"`
	Number          string          `json:"number"`
	ShippingChanged bool            `json:"shippingChanged"`
	FinanceChanged  bool            `json:"financeChanged"`
	ShippingBefore  json.RawMessage `json:"shippingBefore"`
	ShippingAfter   json.RawMessage `json:"shippingAfter,omitempty"`
	FinanceBefore   json.RawMessage `json:"financeBefore,omitempty"`
	FinanceAfter    json.RawMessage `json:"financeAfter,omitempty"`
	PaymentsBefore  json.RawMessage `json:"paymentsBefore,omitempty"`
	PaymentsAfter   json.RawMessage `json:"paymentsAfter,omitempty"`
}

type report struct {
	Tenant          int64   `json:"tenant"`
	DryRun          bool    `json:"dryRun"`
	ShippingChanged int     `json:"shippingChanged"`
	FinanceChanged  int     `json:"financeChanged"`
	Rows            []audit `json:"rows"`
}

func onlyMetadataChanged(before, after []byte, fields ...string) error {
	var a, b map[string]any
	if err := json.Unmarshal(before, &a); err != nil {
		return err
	}
	if err := json.Unmarshal(after, &b); err != nil {
		return err
	}
	for _, key := range fields {
		delete(a, key)
		delete(b, key)
	}
	if !reflect.DeepEqual(a, b) {
		return errors.New("non-metadata fields changed; refusing to commit")
	}
	return nil
}

func payments(ctx context.Context, tx pgx.Tx, tenant, id int64) (json.RawMessage, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]'::jsonb) FROM payment_allocations a WHERE a.tenant_id=$1 AND a.po_id=$2`, tenant, id).Scan(&raw)
	return raw, err
}

func run(ctx context.Context, ship, finance *pgxpool.Pool, tenant, handoff int64, apply bool) (report, error) {
	out := report{Tenant: tenant, DryRun: !apply, Rows: []audit{}}
	st, err := ship.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer st.Rollback(ctx) //nolint:errcheck
	ft, err := finance.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer ft.Rollback(ctx) //nolint:errcheck
	rows, err := st.Query(ctx, `SELECT h.id,h.contract_no,h.forwarder_contract_no,to_jsonb(h) FROM contract_shipping_handoffs h WHERE h.tenant_id=$1 AND ($2::bigint=0 OR h.id=$2) AND h.signed_contract_key<>'' AND h.signed_contract_name<>'' AND h.signed_contract_uploaded_at IS NOT NULL AND (btrim(h.forwarder_contract_no)='' OR h.forwarder_contract_no_generated) ORDER BY h.id FOR UPDATE`, tenant, handoff)
	if err != nil {
		return out, err
	}
	// Drain the rows before issuing updates on the same transaction connection.
	var candidates []audit
	for rows.Next() {
		var a audit
		if err = rows.Scan(&a.HandoffID, &a.ContractNo, &a.Number, &a.ShippingBefore); err != nil {
			rows.Close()
			return out, err
		}
		candidates = append(candidates, a)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	for _, a := range candidates {
		var existing string
		var generated bool
		err = ft.QueryRow(ctx, `SELECT p.id,p.business_document_no,p.business_document_no_generated,to_jsonb(p) FROM purchase_orders p WHERE p.tenant_id=$1 AND p.business_type='LOGISTICS' AND p.source_business_id=$2 FOR UPDATE`, tenant, a.HandoffID).Scan(&a.PayableID, &existing, &generated, &a.FinanceBefore)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		// Never overwrite a real or conflicting finance contract number.
		if existing != "" && (a.Number == "" || existing != a.Number) {
			return out, fmt.Errorf("conflicting finance number for handoff %d; no changes committed", a.HandoffID)
		}
		a.ShippingChanged = strings.TrimSpace(a.Number) == ""
		a.FinanceChanged = a.PayableID > 0 && (a.ShippingChanged || existing != a.Number || !generated)
		if !a.ShippingChanged && !a.FinanceChanged {
			continue
		}
		if a.ShippingChanged {
			out.ShippingChanged++
		}
		if a.FinanceChanged {
			out.FinanceChanged++
		}
		if a.PayableID > 0 {
			a.PaymentsBefore, err = payments(ctx, ft, tenant, a.PayableID)
			if err != nil {
				return out, err
			}
		}
		if apply {
			if a.ShippingChanged {
				err = st.QueryRow(ctx, `UPDATE contract_shipping_handoffs SET forwarder_contract_no=(SELECT 'LC-'||to_char(now(),'YYYYMM')||'-'||lpad(n::text,greatest(6,length(n::text)),'0') FROM (SELECT nextval('logistics_contract_number_seq') AS n) seq),forwarder_contract_no_generated=true WHERE tenant_id=$1 AND id=$2 AND btrim(forwarder_contract_no)='' RETURNING forwarder_contract_no,to_jsonb(contract_shipping_handoffs)`, tenant, a.HandoffID).Scan(&a.Number, &a.ShippingAfter)
				if err != nil {
					return out, err
				}
			} else {
				a.ShippingAfter = a.ShippingBefore
			}
			if err = onlyMetadataChanged(a.ShippingBefore, a.ShippingAfter, "forwarder_contract_no", "forwarder_contract_no_generated"); err != nil {
				return out, fmt.Errorf("handoff %d: %w", a.HandoffID, err)
			}
			if a.FinanceChanged {
				err = ft.QueryRow(ctx, `UPDATE purchase_orders SET business_document_no=$3,business_document_no_generated=true WHERE tenant_id=$1 AND id=$2 RETURNING to_jsonb(purchase_orders)`, tenant, a.PayableID, a.Number).Scan(&a.FinanceAfter)
				if err != nil {
					return out, err
				}
				if err = onlyMetadataChanged(a.FinanceBefore, a.FinanceAfter, "business_document_no", "business_document_no_generated"); err != nil {
					return out, fmt.Errorf("payable %d: %w", a.PayableID, err)
				}
			} else {
				a.FinanceAfter = a.FinanceBefore
			}
			if a.PayableID > 0 {
				a.PaymentsAfter, err = payments(ctx, ft, tenant, a.PayableID)
				if err != nil {
					return out, err
				}
				if !reflect.DeepEqual(a.PaymentsBefore, a.PaymentsAfter) {
					return out, errors.New("payment allocations changed; refusing to commit")
				}
			}
		}
		out.Rows = append(out.Rows, a)
	}
	if apply {
		// The source commits first. If the second commit fails, a rerun picks up
		// the already generated number instead of allocating a replacement.
		if err = st.Commit(ctx); err != nil {
			return out, err
		}
		if err = ft.Commit(ctx); err != nil {
			return out, fmt.Errorf("finance commit failed; rerun to sync existing source numbers: %w", err)
		}
	}
	return out, nil
}

func main() {
	tenant := flag.Int64("tenant", 0, "required tenant ID")
	handoff := flag.Int64("handoff", 0, "optional single handoff for a test run")
	apply := flag.Bool("apply", false, "write number and generated flag only; default is dry run")
	path := flag.String("report", "", "required local JSON audit report path")
	flag.Parse()
	if *tenant <= 0 || *handoff < 0 || *path == "" {
		fmt.Fprintln(os.Stderr, "require -tenant > 0 and -report; -handoff must be nonnegative")
		os.Exit(1)
	}
	if os.Getenv("SHIPPING_BACKFILL_DSN") == "" || os.Getenv("PROCUREMENT_BACKFILL_DSN") == "" {
		fmt.Fprintln(os.Stderr, "both backfill database connections must be explicitly configured")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ship, err := pgxpool.New(ctx, os.Getenv("SHIPPING_BACKFILL_DSN"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipping connection configuration invalid")
		os.Exit(1)
	}
	defer ship.Close()
	finance, err := pgxpool.New(ctx, os.Getenv("PROCUREMENT_BACKFILL_DSN"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "finance connection configuration invalid")
		os.Exit(1)
	}
	defer finance.Close()
	out, err := run(ctx, ship, finance, *tenant, *handoff, *apply)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err == nil {
		err = os.WriteFile(*path, data, 0600)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit report could not be written")
		os.Exit(1)
	}
	fmt.Printf("tenant=%d dryRun=%t shipping=%d finance=%d; metadata invariants verified\n", out.Tenant, out.DryRun, out.ShippingChanged, out.FinanceChanged)
}
