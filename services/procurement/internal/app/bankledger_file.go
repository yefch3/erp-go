package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
)

// 银行对账单那张纸。
//
// 流水行是员工敲进来的数，对账单是银行出的凭证——两者对不上的时候，说了算
// 的是纸。一行一份：再传一次是替换，不是追加（和供应商发票扫描件同一个
// 道理，也是同一套代码，只换了 key 的前缀）。
//
// 整条路是「先拿直传地址 → 浏览器直接传给对象存储 → 回来登记 key」：文件
// 不经过我们的服务，几十兆的全月流水也不会把网关撑爆。

// BankFilePresign 是一次短命的上传许可。
type BankFilePresign struct {
	Key       string
	UploadURL string
	Expires   int32
}

// PresignBankTransactionFile 发一个 key 和一个上传地址。
//
// 这一步**什么都不记**：传到一半放弃的上传不会在流水行上留下任何痕迹，
// 只在桶里留一个没人引用的对象，将来扫一遍就能收走。
func (s *Service) PresignBankTransactionFile(ctx context.Context, tenantID, txnID int64, fileName string, op Operator) (BankFilePresign, error) {
	if s.files == nil {
		return BankFilePresign{}, apierr.Conflict("BANK_FILES_UNAVAILABLE", "文件存储未配置")
	}
	if strings.TrimSpace(fileName) == "" {
		return BankFilePresign{}, apierr.Invalid("BANK_FILE_NAME_REQUIRED", "文件名必填")
	}
	// 先确认这一行真的存在且属于这个租户——不然会发出一个指向不存在的流水
	// 的上传许可，传上去的东西谁也认领不了。
	if _, err := s.GetBankTransaction(ctx, tenantID, txnID, op); err != nil {
		return BankFilePresign{}, err
	}
	key := bankTxnObjectKey(tenantID, txnID, fileName)
	url, expires, err := s.files.PresignPut(ctx, key)
	if err != nil {
		return BankFilePresign{}, err
	}
	return BankFilePresign{Key: key, UploadURL: url, Expires: expires}, nil
}

// AttachBankTransactionFile 把浏览器已经传好的那个对象记到流水行上。
//
// 允许替换——第一次拍糊了是常事——旧对象在行指向新的之后再尽力删除：行指
// 向一个不存在的对象是坏链接，而没人引用的对象只是垃圾。
func (s *Service) AttachBankTransactionFile(ctx context.Context, tenantID, txnID int64, key string, op Operator) (BankTransactionView, error) {
	if s.files == nil {
		return BankTransactionView{}, apierr.Conflict("BANK_FILES_UNAVAILABLE", "文件存储未配置")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return BankTransactionView{}, apierr.Invalid("BANK_FILE_KEY_REQUIRED", "文件标识必填")
	}
	// **这道检查是唯一的跨租户隔离手段，不是洁癖。** 所有服务共用一个桶，
	// 没有它，任何人都能把别人租户的对象登记到自己的流水行上，然后从自己的
	// 页面上把它读出来。前缀相等之外还挡 ".."，理由见 objectkey.go。
	if !objectKeyBelongsTo(key, bankTxnKeyPrefix(tenantID, txnID)) {
		return BankTransactionView{}, apierr.Invalid("BANK_FILE_KEY_MISMATCH", "文件标识与流水不匹配")
	}
	if _, err := s.GetBankTransaction(ctx, tenantID, txnID, op); err != nil {
		return BankTransactionView{}, err
	}
	var previous string
	err := s.pool.QueryRow(ctx, `
		UPDATE bank_transactions AS n SET attachment_key = $3
		  FROM bank_transactions AS o
		 WHERE n.id = o.id AND n.tenant_id = $1 AND n.id = $2
		RETURNING o.attachment_key`,
		tenantID, txnID, key).Scan(&previous)
	if err == pgx.ErrNoRows {
		return BankTransactionView{}, apierr.NotFound("BANK_TXN_NOT_FOUND", "银行流水不存在")
	}
	if err != nil {
		return BankTransactionView{}, err
	}
	if previous != "" && previous != key {
		// 先改行、再删对象；删不掉不能让整个动作失败。
		_ = s.files.Remove(ctx, previous)
	}
	s.nudge(ctx, tenantID)
	return s.GetBankTransaction(ctx, tenantID, txnID, op)
}

// signAttachment 给一行流水现签一个能打开对账单的临时地址。
//
// 签不出来就留空——附件打不开是件小事，为它让整张列表 500 是件大事。
func (s *Service) signAttachment(ctx context.Context, v *BankTransactionView) {
	if v.AttachmentKey == "" {
		return
	}
	v.AttachmentName = bankTxnDisplayName(v.AttachmentKey)
	if s.files == nil {
		return
	}
	if url, err := s.files.PresignGet(ctx, v.AttachmentKey); err == nil {
		v.AttachmentURL = url
	}
}

// bankTxnDisplayName 从 key 里还原原始文件名。前缀那段是十六进制，
// 所以第一个 '-' 一定是分隔符，不会是文件名自己的一部分。
func bankTxnDisplayName(key string) string {
	if key == "" {
		return ""
	}
	base := path.Base(key)
	if _, name, ok := strings.Cut(base, "-"); ok {
		return name
	}
	return base
}

func bankTxnKeyPrefix(tenantID, txnID int64) string {
	return fmt.Sprintf("bank-transactions/%d/%d/", tenantID, txnID)
}

// bankTxnObjectKey 按租户和流水分目录，扫一眼桶就知道一个对象是谁的；
// 前面那段随机数让「同名文件重传一次」不会覆盖掉行还指着的那个旧对象。
func bankTxnObjectKey(tenantID, txnID int64, fileName string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	base := path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	return bankTxnKeyPrefix(tenantID, txnID) + hex.EncodeToString(buf) + "-" + base
}
