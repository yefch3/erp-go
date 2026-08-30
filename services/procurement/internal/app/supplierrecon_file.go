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

// 供应商对账页上的凭证。
//
// 需求原话：「供应商发票只是员工用来上传留凭证用的」。所以这里存的是**纸**，
// 不是一张有金额、有明细、参与运算的发票单据——它不进任何求和，只是「这笔钱
// 是怎么回事」的证据。
//
// 一张采购单可以有好几份（发票、水单、退款回执，分几次到手），所以是列表，
// 不是银行流水那样的一行一份。
//
// 整条路和别处一样：先拿直传地址 → 浏览器直接传给对象存储 → 回来登记 key。
// 文件不经过我们的服务。

// ReconFile 是挂在一张采购单上的一份凭证。
type ReconFile struct {
	ID             int64
	FileName       string
	Note           string
	URL            string
	UploadedByName string
	UploadedAt     string
}

// ReconFilePresign 是一次短命的上传许可。
type ReconFilePresign struct {
	Key       string
	UploadURL string
	Expires   int32
}

// PresignReconFile 发一个 key 和一个上传地址。
//
// 这一步**什么都不记**：传到一半放弃的上传不会在采购单上留下任何痕迹，
// 只在桶里留一个没人引用的对象。
func (s *Service) PresignReconFile(ctx context.Context, tenantID, poID int64,
	fileName string, op Operator) (ReconFilePresign, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return ReconFilePresign{}, err
	}
	if s.files == nil {
		return ReconFilePresign{}, apierr.Conflict("PR_FILES_UNAVAILABLE", "文件存储未配置")
	}
	if strings.TrimSpace(fileName) == "" {
		return ReconFilePresign{}, apierr.Invalid("PR_FILE_NAME_REQUIRED", "文件名必填")
	}
	// 先确认这张采购单真的存在且属于这个租户——不然会发出一个指向不存在的
	// 单子的上传许可，传上去的东西谁也认领不了。
	if _, err := s.reconRowOf(ctx, tenantID, poID); err != nil {
		return ReconFilePresign{}, err
	}
	key := reconObjectKey(tenantID, poID, fileName)
	url, expires, err := s.files.PresignPut(ctx, key)
	if err != nil {
		return ReconFilePresign{}, err
	}
	return ReconFilePresign{Key: key, UploadURL: url, Expires: expires}, nil
}

// AttachReconFile 把浏览器已经传好的那个对象记到采购单上。
func (s *Service) AttachReconFile(ctx context.Context, tenantID, poID int64,
	key, fileName, note string, op Operator) ([]ReconFile, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return nil, err
	}
	if s.files == nil {
		return nil, apierr.Conflict("PR_FILES_UNAVAILABLE", "文件存储未配置")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, apierr.Invalid("PR_FILE_KEY_REQUIRED", "文件标识必填")
	}
	// **这道检查是唯一的跨租户隔离手段，不是洁癖。** 所有服务共用一个桶，
	// 没有它，任何人都能把别人租户的对象登记到自己的采购单上，然后从自己的
	// 页面上把它读出来。前缀相等之外还挡 ".."，理由见 objectkey.go。
	if !objectKeyBelongsTo(key, reconKeyPrefix(tenantID, poID)) {
		return nil, apierr.Invalid("PR_FILE_KEY_MISMATCH", "文件标识与采购单不匹配")
	}
	if _, err := s.reconRowOf(ctx, tenantID, poID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(fileName)
	if name == "" {
		name = reconDisplayName(key)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO purchase_order_recon_files
		  (tenant_id, po_id, object_key, file_name, note, uploaded_by_id, uploaded_by_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		tenantID, poID, key, name, strings.TrimSpace(note), op.ID, op.Name); err != nil {
		return nil, err
	}
	s.nudge(ctx, tenantID)
	return s.ListReconFiles(ctx, tenantID, poID, op)
}

// ListReconFiles 列出一张采购单上还挂着的凭证，附带临时可读地址。
func (s *Service) ListReconFiles(ctx context.Context, tenantID, poID int64,
	op Operator) ([]ReconFile, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, object_key, file_name, note, uploaded_by_name, created_at::text
		  FROM purchase_order_recon_files
		 WHERE tenant_id = $1 AND po_id = $2 AND removed_at IS NULL
		 ORDER BY id`, tenantID, poID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReconFile
	for rows.Next() {
		var f ReconFile
		var key string
		if err := rows.Scan(&f.ID, &key, &f.FileName, &f.Note,
			&f.UploadedByName, &f.UploadedAt); err != nil {
			return nil, err
		}
		if f.FileName == "" {
			f.FileName = reconDisplayName(key)
		}
		// 签不出来就留空——一份凭证打不开是件小事，为它让整个列表 500
		// 是件大事。
		if s.files != nil {
			if url, err := s.files.PresignGet(ctx, key); err == nil {
				f.URL = url
			}
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// RemoveReconFile 把一份凭证撤下来。
//
// 不删行、也不删对象：谁在什么时候撤的要留得住，而对象还留着意味着「撤错了」
// 是可以补救的。桶里的垃圾将来扫一遍就能收走，账上的空白补不回来。
func (s *Service) RemoveReconFile(ctx context.Context, tenantID, fileID int64,
	op Operator) ([]ReconFile, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return nil, err
	}
	var poID int64
	err := s.pool.QueryRow(ctx, `
		UPDATE purchase_order_recon_files
		   SET removed_at = now(), removed_by_id = $3, removed_by_name = $4
		 WHERE tenant_id = $1 AND id = $2 AND removed_at IS NULL
		RETURNING po_id`, tenantID, fileID, op.ID, op.Name).Scan(&poID)
	if err == pgx.ErrNoRows {
		return nil, apierr.NotFound("PR_FILE_NOT_FOUND", "这份凭证不存在或已经撤下来了")
	}
	if err != nil {
		return nil, err
	}
	s.nudge(ctx, tenantID)
	return s.ListReconFiles(ctx, tenantID, poID, op)
}

// reconDisplayName 从 key 里还原原始文件名。前缀那段是十六进制，所以第一个
// '-' 一定是分隔符，不会是文件名自己的一部分。
func reconDisplayName(key string) string {
	if key == "" {
		return ""
	}
	base := path.Base(key)
	if _, name, ok := strings.Cut(base, "-"); ok {
		return name
	}
	return base
}

func reconKeyPrefix(tenantID, poID int64) string {
	return fmt.Sprintf("po-recon/%d/%d/", tenantID, poID)
}

// reconObjectKey 按租户和采购单分目录，扫一眼桶就知道一个对象是谁的；
// 前面那段随机数让「同名文件传两次」不会覆盖掉上一份。
func reconObjectKey(tenantID, poID int64, fileName string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	base := path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	return reconKeyPrefix(tenantID, poID) + hex.EncodeToString(buf) + "-" + base
}
