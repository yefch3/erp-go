package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/services/product/internal/store"
)

// Recall sources, carried on every candidate so a reviewer can see why it
// came back. A candidate nobody can explain is a candidate nobody trusts.
const (
	SourceAttr     = "ATTR"
	SourceFullText = "FULLTEXT"
)

// AttrQuery is one line of a customer's request, after parsing.
type AttrQuery struct {
	CategoryID int64
	// Attribute values as parsed. Keys that no definition covers are ignored
	// rather than rejected — a request line may mention things we do not model.
	Values map[string]any
	// The raw line, used for the text pass.
	Keyword string
	Limit   int32
}

// Candidate is one product/SKU that might be what the customer asked for.
type Candidate struct {
	ProductID    int64
	SkuID        int64
	ProductCode  string
	ProductName  string
	SkuCode      string
	Spec         string
	Attributes   map[string]any
	Score        string
	RecallSource string
	// Why this one came back, in words. Filled per source.
	Reason string
}

// Recall returns everything that might match, ranked.
//
// Two passes, unioned: structured attribute matching and trigram text. Recall
// beats precision here by an explicit business decision — a missed candidate
// costs a wrong quotation, an extra one costs a glance. So nothing is
// filtered out for scoring low; low scores sort to the bottom.
//
// The structured pass reads the category's template to decide how each field
// is compared, which is what lets steel and consumer goods share one engine.
func (s *Service) Recall(ctx context.Context, tenantID int64, qy AttrQuery) ([]Candidate, error) {
	limit := qy.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	defs, err := s.ResolveDefs(ctx, tenantID, qy.CategoryID)
	if err != nil {
		return nil, err
	}

	out := make([]Candidate, 0, limit*2)
	seen := make(map[[2]int64]bool)

	// Structured first: when it hits, it is the answer, so it ranks above text.
	if len(defs) > 0 && len(qy.Values) > 0 {
		rows, err := s.structuredRecall(ctx, tenantID, qy, defs, limit)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			key := [2]int64{r.ProductID, r.SkuID}
			seen[key] = true
			out = append(out, Candidate{
				ProductID: r.ProductID, SkuID: r.SkuID,
				ProductCode: r.ProductCode, ProductName: r.ProductName,
				SkuCode: r.SkuCode, Spec: r.Spec,
				Attributes:   mergeAttrs(r.ProductAttributes, r.SkuAttributes),
				Score:        "1",
				RecallSource: SourceAttr,
				Reason:       "规格逐项匹配",
			})
		}
	}

	// Text always runs. It is the only pass a tenant with no template has,
	// and even with a template it catches the lines whose wording did not
	// parse into attributes at all.
	if qy.Keyword != "" {
		rows, err := s.q.SearchSkusByText(ctx, store.SearchSkusByTextParams{
			TenantID: tenantID, Keyword: qy.Keyword, RowLimit: limit,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			key := [2]int64{r.ProductID, r.SkuID}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Candidate{
				ProductID: r.ProductID, SkuID: r.SkuID,
				ProductCode: r.ProductCode, ProductName: r.ProductName,
				SkuCode: r.SkuCode, Spec: r.Spec,
				Score:        r.Score,
				RecallSource: SourceFullText,
				Reason:       "名称或规格文本相近",
			})
		}
	}
	return out, nil
}

// structuredRecall turns parsed values into a query, one condition per
// definition, using the strategy the template declares.
func (s *Service) structuredRecall(
	ctx context.Context, tenantID int64, qy AttrQuery, defs []AttributeDef, limit int32,
) ([]store.SearchSkusByAttributesRow, error) {
	productExact := map[string]any{}
	skuExact := map[string]any{}
	ranges := make([]map[string]string, 0, 4)

	for _, d := range defs {
		if !d.IsMatchable {
			continue
		}
		raw, ok := qy.Values[d.Key]
		if !ok || raw == nil || raw == "" {
			continue
		}
		switch d.MatchStrategy {
		case "TOLERANCE":
			n, err := toNumber(raw)
			if err != nil {
				continue // not a number after all; the text pass still sees it
			}
			pct, err := decimal.NewFromString(d.TolerancePct)
			if err != nil {
				pct = decimal.Zero
			}
			band := n.Mul(pct).Div(decimal.NewFromInt(100)).Abs()
			ranges = append(ranges, map[string]string{
				"key": d.Key,
				"min": n.Sub(band).String(),
				"max": n.Add(band).String(),
			})
		case "FUZZY":
			// Left to the text pass on purpose: a LIKE inside the structured
			// query would narrow the result set, and this pass exists to
			// widen it.
			continue
		default:
			// EXACT, SYNONYM and anything else fall back to containment.
			// SYNONYM will get an alias table in 9.3; until then it behaves
			// as EXACT, which is honest rather than silently approximate.
			value := normaliseForMatch(d, raw)
			if d.Level == LevelProduct {
				productExact[d.Key] = value
			} else {
				skuExact[d.Key] = value
			}
		}
	}

	productJSON, err := json.Marshal(productExact)
	if err != nil {
		return nil, err
	}
	skuJSON, err := json.Marshal(skuExact)
	if err != nil {
		return nil, err
	}
	rangeJSON, err := json.Marshal(ranges)
	if err != nil {
		return nil, err
	}
	return s.q.SearchSkusByAttributes(ctx, store.SearchSkusByAttributesParams{
		TenantID: tenantID, CategoryID: qy.CategoryID,
		ProductExact: productJSON, SkuExact: skuJSON, Ranges: rangeJSON,
		RowLimit: limit,
	})
}

// normaliseForMatch makes the query value look like the stored value.
// Numbers are stored as JSON numbers, so "3.0" from a parsed line has to
// become 3.0 or containment will never hit.
func normaliseForMatch(d AttributeDef, raw any) any {
	switch d.DataType {
	case "DIMENSION", "NUMBER", "RANGE":
		if n, err := toNumber(raw); err == nil {
			f, _ := n.Float64()
			return f
		}
	}
	return fmt.Sprint(raw)
}

func mergeAttrs(productRaw, skuRaw []byte) map[string]any {
	out := map[string]any{}
	for _, raw := range [][]byte{productRaw, skuRaw} {
		if len(raw) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
