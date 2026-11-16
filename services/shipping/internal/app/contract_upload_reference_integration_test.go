package app

import (
	"fmt"
	"regexp"
	"testing"
)

func TestShippingContractFileOnlyUploadPreservesLegacyNumber(t *testing.T) {
	ctx, pool, tenant := deliveryTestDB(t)
	svc := New(pool)
	svc.files = documentFilesStub{size: 1024, contentType: "application/pdf"}
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO contract_shipping_handoffs(tenant_id,contract_id,contract_no,contract_version_id,version_no,batch_no,currency,status) VALUES($1,99,'CT-UPLOAD',1,1,1,'USD','APPROVED') RETURNING id`, tenant).Scan(&id); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("d4-contracts/shipping/%d/%d/signed.pdf", tenant, id)
	v, err := svc.SaveShippingContract(ctx, tenant, id, key, "signed.pdf", "")
	if err != nil || v.Status != "CONTRACT_UPLOADED" || !v.ForwarderContractNoGenerated || !regexp.MustCompile(`^LC-\d{6}-\d{6,}$`).MatchString(v.ForwarderContractNo) {
		t.Fatalf("file-only upload: %+v, %v", v, err)
	}
	generated := v.ForwarderContractNo
	for _, submitted := range []string{"", generated} {
		v, err = svc.SaveShippingContract(ctx, tenant, id, key, "reuploaded.pdf", submitted)
		if err != nil || v.ForwarderContractNo != generated || !v.ForwarderContractNoGenerated {
			t.Fatalf("reupload changed generated identity: %+v, %v", v, err)
		}
	}
	var secondID int64
	if err = pool.QueryRow(ctx, `INSERT INTO contract_shipping_handoffs(tenant_id,contract_id,contract_no,contract_version_id,version_no,batch_no,currency,status) VALUES($1,99,'CT-UPLOAD',1,1,2,'USD','APPROVED') RETURNING id`, tenant).Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	second, err := svc.SaveShippingContract(ctx, tenant, secondID, fmt.Sprintf("d4-contracts/shipping/%d/%d/signed.pdf", tenant, secondID), "signed.pdf", "")
	if err != nil || second.ForwarderContractNo == generated || !second.ForwarderContractNoGenerated {
		t.Fatalf("different records reused an automatic number: %+v, %v", second, err)
	}
	if _, err = svc.SaveShippingContract(ctx, tenant, id, key, "signed.pdf", "LEGACY-99"); err != nil {
		t.Fatal(err)
	}
	v, err = svc.SaveShippingContract(ctx, tenant, id, key, "replacement.pdf", "")
	if err != nil || v.ForwarderContractNo != "LEGACY-99" || v.ForwarderContractNoGenerated || v.SignedContractName != "replacement.pdf" {
		t.Fatalf("replacement erased legacy metadata: %+v, %v", v, err)
	}
}
