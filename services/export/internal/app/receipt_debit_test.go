package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// 出账在这里暂时是拒收的——不是永远，是因为它今天没有去处：本页列表写死
// 只出进账，归属又被无条件写成客户，登记一笔出账等于把钱录进一个谁都看
// 不见的角落。收付队列改造给客户退款开了子页面之后，这条测试会被删掉。
//
// 纯单元测试：这道闸必须在碰账本之前就关上（构造里 bank 是 nil，闸要是
// 晚了这里会直接空指针崩掉——这也是测试的一部分）。
func TestRecordingADebitIsRefusedLoudlyForNow(t *testing.T) {
	svc := New(nil, Deps{})
	_, err := svc.RecordTransaction(context.Background(), 1, TransactionInput{
		AccountID: 1, BankRef: "X-1", Direction: "DEBIT",
		Amount: "100", Currency: "USD", ValueDate: "2026-08-22",
	}, Operator{ID: 7})
	if err == nil {
		t.Fatal("一笔出账被收下了——它会从每一个页面上消失")
	}
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Code != "EX_TX_DEBIT_NOT_YET" {
		t.Fatalf("拒绝的理由不对：%v", err)
	}
}
