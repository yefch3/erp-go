package app

import (
	"context"
	"strings"
)

type ShippingContractReference struct {
	ID                             int64
	ContractNo, ExternalContractNo string
}

// Shipping sees only company-local reference numbers, never sales prices/files.
func (s *Service) ShippingContractReferences(ctx context.Context, tenant, id int64, keyword string, exact bool) ([]ShippingContractReference, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,contract_no,external_contract_no FROM contracts WHERE tenant_id=$1 AND ($2::bigint=0 OR id=$2) AND ($3='' OR (CASE WHEN $4::boolean THEN contract_no=$3 OR external_contract_no=$3 ELSE strpos(lower(contract_no),lower($3))>0 OR strpos(lower(external_contract_no),lower($3))>0 END)) ORDER BY id DESC LIMIT 50`, tenant, id, strings.TrimSpace(keyword), exact)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShippingContractReference{}
	for rows.Next() {
		var r ShippingContractReference
		if err = rows.Scan(&r.ID, &r.ContractNo, &r.ExternalContractNo); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
