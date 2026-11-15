package httpapi

import (
	"fmt"
	"strings"
)

func shippingBusinessNo(contractNo string, batchNo int32) string {
	label := strings.TrimSpace(contractNo)
	if label == "" {
		label = "未关联合同"
	}
	if batchNo > 0 {
		return fmt.Sprintf("%s · 第 %d 批", label, batchNo)
	}
	return label + " · 批次未关联"
}
