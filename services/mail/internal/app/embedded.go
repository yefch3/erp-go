package app

import (
	"bytes"
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// Pictures a message carries inside itself.
//
// A signature logo is not fetched from anywhere. It travels as a part of the
// message, and the body points at it with src="cid:<identifier>" — Content-ID,
// a name that means something only within that one mail. The browser has never
// understood cid: and never will; it is an email idea, not a web one.
//
// So the bytes were in object storage, the body asked for them by name, and
// nothing joined the two: 116 messages here, 572 broken-image glyphs. The join
// is the identifier, which ingest used to discard.
//
// Everything below reuses the machinery built for remote pictures — the same
// substitution function, the same signed storage URLs. Only the key changes:
// there the lookup is by full address, here by identifier.

// cidPrefix is what a body writes in front of the identifier.
const cidPrefix = "cid:"

// embeddedAttachmentFragment connects the picture displayed in the sandboxed
// reader back to the already owner-scoped attachment row accepted by the
// Excel conversion endpoint. URL fragments are not sent to object storage,
// so this metadata neither changes nor invalidates the signed download URL.
const embeddedAttachmentFragment = "erp-mail-attachment="

func markEmbeddedAttachment(url string, attachmentID int64) string {
	if url == "" || attachmentID <= 0 {
		return url
	}
	return url + "#" + embeddedAttachmentFragment + strconv.FormatInt(attachmentID, 10)
}

// embeddedSwap builds the cid: → signed URL entries for one message.
func (s *Service) embeddedSwap(ctx context.Context, tenantID, inboundID int64) imageSwap {
	if s.files == nil {
		return nil
	}
	rows, err := s.q.ListInboundEmbedded(ctx, store.ListInboundEmbeddedParams{
		TenantID: tenantID, InboundID: inboundID,
	})
	if err != nil || len(rows) == 0 {
		return nil
	}
	swap := make(imageSwap, len(rows))
	for _, r := range rows {
		if u := s.signImage(ctx, r.FileKey, r.ContentType); u != "" {
			swap[cidPrefix+r.ContentID] = markEmbeddedAttachment(u, r.ID)
		}
	}
	return swap
}

// embeddedSwapForThread does the same for every turn of a conversation.
func (s *Service) embeddedSwapForThread(ctx context.Context, tenantID, ownerID int64, threadKey string) map[int64]imageSwap {
	if s.files == nil || threadKey == "" {
		return nil
	}
	rows, err := s.q.ListThreadEmbedded(ctx, store.ListThreadEmbeddedParams{
		TenantID: tenantID, OwnerID: ownerID, ThreadKey: threadKey,
	})
	if err != nil || len(rows) == 0 {
		return nil
	}
	signed := map[string]string{}
	out := map[int64]imageSwap{}
	for _, r := range rows {
		u, ok := signed[r.FileKey]
		if !ok {
			u = s.signImage(ctx, r.FileKey, r.ContentType)
			signed[r.FileKey] = u
		}
		if u == "" {
			continue
		}
		if out[r.InboundID] == nil {
			out[r.InboundID] = imageSwap{}
		}
		out[r.InboundID][cidPrefix+r.ContentID] = markEmbeddedAttachment(u, r.ID)
	}
	return out
}

// mergeSwaps folds the embedded entries in with the cached-remote ones so the
// body is rewritten in a single pass. Both kinds are just "this src becomes
// that URL"; keeping them apart would mean walking the markup twice.
func mergeSwaps(a, b imageSwap) imageSwap {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make(imageSwap, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// ------------------------------------------------------- the attachment list

// bodyCIDs is every identifier a body points at.
var cidRef = regexp.MustCompile(`(?i)src\s*=\s*["']cid:([^"']+)["']`)

func bodyCIDs(html string) map[string]bool {
	out := map[string]bool{}
	for _, m := range cidRef.FindAllStringSubmatch(html, -1) {
		if id := strings.TrimSpace(m[1]); id != "" {
			out[id] = true
		}
	}
	return out
}

// hasListedAttachments 是列表上那枚回形针该不该亮。
//
// 和 hideEmbedded 同一条规矩：正文里 <img src="cid:X"> 指着的那个部件是
// 签名的 logo，不是附件——它在读信页上会被藏起来，列表上再亮一枚回形针
// 等于告诉人「有附件」，点进去却什么都没有。用的人问过「明明没有附件，
// 为什么有图标」。
//
// 只看「正文指没指着它」，不看发件方标的 inline：客户端给真附件标 inline
// 的事天天发生，按那个标记算会把合同算没。
//
// 少 hideEmbedded 那一条「换出 URL 才藏」的条件，是有意的：入库时还不知道
// 存储那边会不会成功；存储失败时读信页会把它列出来（能下载总比不见了
// 强），而列表上没有回形针——这是那条失败路径上可以接受的一点不一致。
func hasListedAttachments(parsed ParsedMail) bool {
	shown := bodyCIDs(parsed.BodyHTML)
	for _, a := range parsed.Attachments {
		if a.ContentID != "" && shown[a.ContentID] {
			continue
		}
		return true
	}
	return false
}

// hideEmbedded drops the parts the body has already shown inline.
//
// A signature logo listed beside the signed contract is noise, and it is the
// second symptom of the same missing identifier — with no way to tell the two
// apart, both were listed.
//
// Two conditions, and both matter.
//
// The body must actually point at the part. Not "did the sender mark it
// inline": clients send Content-Disposition: inline for real attachments all
// the time, and hiding on that flag alone would make a customer's contract
// disappear. A part nobody references stays listed whatever its headers say.
//
// And the substitution must have produced a URL for it. Otherwise a part we
// could not resolve would vanish twice over — stripped from the body by the
// sanitiser and hidden from the list here — leaving a file that arrived and
// is reachable from nowhere. When in doubt it stays in the list, where it can
// at least be downloaded.
func hideEmbedded(atts []Attachment, storedBody string, swap imageSwap) []Attachment {
	if len(atts) == 0 || len(swap) == 0 || !strings.Contains(storedBody, cidPrefix) {
		return atts
	}
	shown := bodyCIDs(storedBody)
	if len(shown) == 0 {
		return atts
	}
	out := make([]Attachment, 0, len(atts))
	for _, a := range atts {
		if a.ContentID != "" && shown[a.ContentID] && swap[cidPrefix+a.ContentID] != "" {
			continue
		}
		out = append(out, a)
	}
	return out
}

// ------------------------------------------------------------- the backfill

const (
	contentIDBatch = 20
	// Between batches. Each message means reading its raw MIME back out of
	// object storage and parsing it, so this is deliberately unhurried: it is
	// repairing history, and history has waited this long.
	contentIDInterval = 3 * time.Second
)

// RunContentIDBackfill repairs messages stored before ingest kept Content-ID.
//
// It reads the original MIME back from object storage — which is what that
// archive is for — re-parses it, and writes the identifiers onto the
// attachment rows that are already there. Nothing is re-downloaded from
// anybody and no new rows appear; only the missing join is filled in.
//
// The queue is a query rather than a marker column: a message qualifies while
// its body names a cid: that none of its attachments answers to, so repairing
// one takes it out of the queue. That also means the pass cannot loop for ever
// on a message it fails to fix — see the guard below.
func (s *Service) RunContentIDBackfill(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	if s.files == nil {
		return
	}
	repaired, skipped := 0, 0
	for {
		rows, err := s.q.ListInboundNeedingContentIDs(ctx, store.ListInboundNeedingContentIDsParams{
			TenantID: cfg.TenantID, RowLimit: contentIDBatch,
		})
		if err != nil {
			s.log.Warn("content-id backfill could not read a batch", "err", err)
			return
		}
		if len(rows) == 0 {
			break
		}
		before := repaired
		for _, r := range rows {
			if ctx.Err() != nil {
				return
			}
			if s.repairContentIDs(ctx, cfg.TenantID, r.ID, r.RawKey) {
				repaired++
			}
		}
		if repaired == before {
			// Every message in the batch failed. Without this the same batch
			// comes back for ever: the queue is defined by the problem, so a
			// message that cannot be fixed never leaves it. Messages whose
			// parts genuinely have no Content-ID land here — a body pointing
			// at a part the sender never labelled is not repairable, and
			// pretending otherwise would spin a loop.
			skipped += len(rows)
			s.log.Info("content-id backfill stopping: the remaining messages cannot be repaired",
				"repaired", repaired, "unrepairable", skipped)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(contentIDInterval):
		}
	}
	if repaired > 0 {
		s.log.Info("content-id backfill finished", "repaired", repaired)
	}
}

// partKey identifies a stored part well enough to recognise it again.
//
// Name and size together, rather than position. Position was the first
// attempt, on the reasoning that the parser is deterministic — but the rows
// were written by whatever version of the parser ran the day the mail
// arrived, and this parse is today's. Twenty-five messages had a different
// number of parts on the two readings and were abandoned wholesale, including
// several where every name still matched perfectly.
//
// Keying by name and size fixes those: an extra part appearing or vanishing no
// longer disqualifies the ones that did line up.
type partKey struct {
	name string
	size int64
}

// repairContentIDs re-reads one message and fills in what ingest dropped.
//
// Ambiguity is refused rather than guessed. Two parts with the same name and
// the same size but different identifiers cannot be told apart from here, and
// attaching the wrong picture to a body is worse than leaving it broken.
func (s *Service) repairContentIDs(ctx context.Context, tenantID, inboundID int64, rawKey string) bool {
	raw, err := s.readRaw(ctx, rawKey)
	if err != nil {
		s.log.Warn("content-id backfill could not read the original message",
			"id", inboundID, "err", err)
		return false
	}
	parsed, err := ParseMail(raw)
	if err != nil {
		s.log.Warn("content-id backfill could not parse the original message",
			"id", inboundID, "err", err)
		return false
	}
	rows, err := s.q.ListInboundAttachmentsForRepair(ctx, store.ListInboundAttachmentsForRepairParams{
		TenantID: tenantID, InboundID: inboundID,
	})
	if err != nil || len(rows) == 0 {
		return false
	}

	byPart := map[partKey]string{}
	ambiguous := map[partKey]bool{}
	for _, p := range parsed.Attachments {
		if p.ContentID == "" {
			continue
		}
		k := partKey{p.FileName, int64(len(p.Data))}
		if prior, seen := byPart[k]; seen && prior != p.ContentID {
			ambiguous[k] = true
			continue
		}
		byPart[k] = p.ContentID
	}

	wrote, unmatched := false, 0
	for _, row := range rows {
		k := partKey{row.FileName, row.FileSize}
		id, ok := byPart[k]
		if !ok || ambiguous[k] {
			unmatched++
			continue
		}
		if err := s.q.SetAttachmentContentID(ctx, store.SetAttachmentContentIDParams{
			TenantID: tenantID, ID: row.ID, ContentID: id,
		}); err != nil {
			s.log.Warn("content-id backfill could not write a row", "id", row.ID, "err", err)
			return false
		}
		wrote = true
	}
	if !wrote {
		// Said out loud, because this is the path that fires most often and
		// the first version of it returned silently — which left twenty-five
		// unrepaired messages and nothing in the log to say why.
		s.log.Info("content-id backfill could not match this message's parts",
			"id", inboundID, "stored_parts", len(rows),
			"parts_with_an_id", len(byPart), "unmatched", unmatched)
	}
	return wrote
}

// readRaw pulls a stored message back out of object storage.
func (s *Service) readRaw(ctx context.Context, key string) ([]byte, error) {
	rc, err := s.files.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
