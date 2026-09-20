package app

import (
	"context"
	"fmt"
	"io"
)

// 一次性修补：把互相覆盖掉的收件附件重新摆回各自的位置。
//
// 背景在 inboundAttachmentKey 的注释里：存储位置从前只用「信编号 + 文件名」
// 算，同一封信里两个都叫 image.png 的部件撞在同一个位置，后写的把先写的
// 盖掉，而先写的那一行还指着它。2026-09-20 在生产上数到 461 组、1,015 个
// 文件，其中 909 个是正文内嵌图，显示出来是张牛头不对马嘴的图。
//
// 怎么补：原始邮件完整留着（email_inbound.raw_key，受影响的 346 封一封不
// 少）。把原件重新解一遍，第 n 个附件就写到第 n 个位置去——和现在的代码
// 算出来的位置一模一样，所以补完之后再同步一次也不会多出东西来。
//
// **这是个一次性任务，跑完就该删。** 留着它不删的代价见
// docs 里那条教训：一次性修补不退役，就会变成每次启动都扫全表、而且永远
// 在重试那些修不好的行。所以它不挂在启动上，只作为子命令手动跑一次。
//
// 幂等：位置已经对了的行不碰，可以安全地重复跑。

// RepairOptions 圈定这一趟补哪些。
//
// 有范围可圈，是因为这活儿要在生产上分步走：先挑一封看结果，再放开一家
// 公司，最后全量。一把梭的修补脚本没有中途叫停的余地。
type RepairOptions struct {
	// 只补这家公司；0 = 全部。
	TenantID int64
	// 只补这一封信；0 = 不限。
	InboundID int64
	// 最多处理多少封；0 = 不限。
	Limit int
	// 真为「只看不动」：算出该改成什么、验证原件解得开，但不写对象存储也
	// 不改库。
	DryRun bool
}

// RepairReport 是跑完之后的一张账。
type RepairReport struct {
	MessagesSeen     int
	MessagesRepaired int
	FilesRestored    int
	MessagesSkipped  int
	Problems         []string
}

// repairCandidate 是一封需要补的信。
type repairCandidate struct {
	inboundID int64
	tenantID  int64
	accountID int64
	rawKey    string
}

// RepairInboundAttachmentKeys 找出互相覆盖的附件并把它们摆回去。
func (s *Service) RepairInboundAttachmentKeys(ctx context.Context, opt RepairOptions) (RepairReport, error) {
	var rep RepairReport
	if s.files == nil {
		return rep, fmt.Errorf("repair: 没有接上对象存储，补不了")
	}

	cands, err := s.collidedMessages(ctx, opt)
	if err != nil {
		return rep, err
	}
	rep.MessagesSeen = len(cands)

	for _, c := range cands {
		restored, err := s.repairOneMessage(ctx, c, opt.DryRun)
		if err != nil {
			rep.MessagesSkipped++
			rep.Problems = append(rep.Problems,
				fmt.Sprintf("信 %d（公司 %d）：%v", c.inboundID, c.tenantID, err))
			continue
		}
		if restored > 0 {
			rep.MessagesRepaired++
			rep.FilesRestored += restored
		}
	}
	return rep, nil
}

// collidedMessages 找出「同一封信里有两行指着同一个位置」的那些信。
//
// 写成裸 SQL 而不是 sqlc 查询：这段代码活不过这次修补，不该在 store/ 里
// 留下一个谁都不会再用的方法。
func (s *Service) collidedMessages(ctx context.Context, opt RepairOptions) ([]repairCandidate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT m.id, m.tenant_id, m.account_id, m.raw_key
		FROM email_inbound m
		JOIN email_inbound_attachments a ON a.inbound_id = m.id
		WHERE a.file_key <> ''
		  AND ($1::bigint = 0 OR m.tenant_id = $1::bigint)
		  AND ($2::bigint = 0 OR m.id = $2::bigint)
		  AND EXISTS (
		    SELECT 1 FROM email_inbound_attachments b
		    WHERE b.inbound_id = a.inbound_id AND b.file_key = a.file_key AND b.id <> a.id)
		ORDER BY m.id
		LIMIT CASE WHEN $3::int > 0 THEN $3::int ELSE NULL END`,
		opt.TenantID, opt.InboundID, opt.Limit)
	if err != nil {
		return nil, fmt.Errorf("repair: 找不出要补的信: %w", err)
	}
	defer rows.Close()

	var out []repairCandidate
	for rows.Next() {
		var c repairCandidate
		if err := rows.Scan(&c.inboundID, &c.tenantID, &c.accountID, &c.rawKey); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// repairOneMessage 重解一封信的原件，把每个附件写到它该在的位置。
//
// 返回真正被重新写出去的文件数。
func (s *Service) repairOneMessage(ctx context.Context, c repairCandidate, dryRun bool) (int, error) {
	if c.rawKey == "" {
		return 0, fmt.Errorf("原件没留，补不回来")
	}
	raw, err := s.readAll(ctx, c.rawKey)
	if err != nil {
		return 0, fmt.Errorf("取原件失败: %w", err)
	}
	parsed, err := ParseMail(raw)
	if err != nil {
		return 0, fmt.Errorf("解析原件失败: %w", err)
	}

	// 库里的行和解析出来的附件是一一对应的：当初就是按 parsed.Attachments
	// 的顺序一行行插进去的，所以第 n 行就是第 n 个部件。对不上就不敢动。
	rows, err := s.pool.Query(ctx,
		`SELECT id, file_key FROM email_inbound_attachments WHERE inbound_id = $1 ORDER BY id`, c.inboundID)
	if err != nil {
		return 0, err
	}
	type attRow struct {
		id  int64
		key string
	}
	var stored []attRow
	for rows.Next() {
		var r attRow
		if err := rows.Scan(&r.id, &r.key); err != nil {
			rows.Close()
			return 0, err
		}
		stored = append(stored, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(stored) != len(parsed.Attachments) {
		return 0, fmt.Errorf("原件解出 %d 个附件，库里有 %d 行，对不上，不动",
			len(parsed.Attachments), len(stored))
	}

	restored := 0
	for i, a := range parsed.Attachments {
		row := stored[i]
		// 当初就没存过（太大）的行保持原样：它的空位置是实话。
		if row.key == "" || a.Oversized || len(a.Data) == 0 {
			continue
		}
		want := inboundAttachmentKey(c.tenantID, c.accountID, c.inboundID, i, a.FileName)
		if row.key == want {
			continue // 已经对了
		}
		if dryRun {
			restored++
			continue
		}
		if err := s.putRaw(ctx, want, a.Data); err != nil {
			return restored, fmt.Errorf("写附件 %d（%s）失败: %w", i+1, a.FileName, err)
		}
		if _, err := s.pool.Exec(ctx,
			`UPDATE email_inbound_attachments SET file_key = $1 WHERE id = $2`, want, row.id); err != nil {
			return restored, fmt.Errorf("更新第 %d 行失败: %w", row.id, err)
		}
		restored++
	}
	return restored, nil
}

// readAll 把对象存储里的一个文件整个读进来。
func (s *Service) readAll(ctx context.Context, key string) ([]byte, error) {
	rc, err := s.files.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
