// Package inquiryproof authenticates the narrow procurement-to-export command.
package inquiryproof

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func Sign(key string, tenant, employee, caseID int64, available bool) string {
	if key == "" {
		return ""
	}
	m := hmac.New(sha256.New, []byte(key))
	fmt.Fprintf(m, "inquiry-availability:%d:%d:%d:%t", tenant, employee, caseID, available)
	return hex.EncodeToString(m.Sum(nil))
}
func Verify(key, proof string, tenant, employee, caseID int64, available bool) bool {
	expected := Sign(key, tenant, employee, caseID, available)
	return expected != "" && hmac.Equal([]byte(expected), []byte(proof))
}
