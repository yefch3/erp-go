package app

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// BizTypeShipment is the numbering series these documents draw from.
const BizTypeShipment = "SHIPMENT"

// ShipmentInput is a booking as typed in. Everything about the boat lives on
// the header; everything about the cargo lives on the lines, and each line
// names its own contract — one box routinely carries several.
type ShipmentInput struct {
	VesselName      string
	VoyageNo        string
	BlNo            string
	ContainerNo     string
	PortOfDischarge string
	// ISO dates, empty meaning not yet known. A booking is often made before
	// the carrier confirms either date, and refusing to save it until then
	// would push people back into spreadsheets.
	ETD    string
	ETA    string
	Remark string
	Items  []ShipmentLineInput
}

type ShipmentLineInput struct {
	ContractID     int64
	ContractItemID int64
	Qty            string
}

// ShipmentView is a document with its cargo.
type ShipmentView struct {
	Shipment store.GetShipmentRow
	Items    []store.ItemsOfShipmentRow
}

// CreateShipment records a booking as a draft.
//
// Draft rather than final because the numbers on a booking change up to the
// moment the box is sealed, and because nothing should count as shipped until
// somebody says the boat sailed. Confirmation is the event that moves goods in
// the progress ledger; saving a draft moves nothing.
func (s *Service) CreateShipment(ctx context.Context, tenantID int64, in ShipmentInput, op Operator) (ShipmentView, error) {
	lines, err := s.resolveShipmentLines(ctx, tenantID, in.Items)
	if err != nil {
		return ShipmentView{}, err
	}

	var id int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// Drawn inside the transaction after the lines have been checked, so a
		// refused booking does not burn a number. A shipment series with holes
		// in it is a question somebody has to answer at audit time.
		no, err := s.number.Next(ctx, BizTypeShipment)
		if err != nil {
			return err
		}
		id, err = q.CreateShipment(ctx, store.CreateShipmentParams{
			TenantID: tenantID, ShipmentNo: no,
			VesselName: in.VesselName, VoyageNo: in.VoyageNo, BlNo: in.BlNo,
			ContainerNo: in.ContainerNo, PortOfDischarge: in.PortOfDischarge,
			Etd: in.ETD, Eta: in.ETA, Remark: in.Remark,
			CreatedBy: op.ID, CreatedByName: op.Name,
		})
		if err != nil {
			return translateUnique(err, "EX_SHIPMENT_NO_TAKEN", "出运单号已存在")
		}
		return insertShipmentLines(ctx, q, tenantID, id, lines)
	})
	if err != nil {
		return ShipmentView{}, err
	}
	return s.GetShipmentDoc(ctx, tenantID, id)
}

// UpdateShipment replaces a draft's header and cargo wholesale.
//
// Wholesale because a booking is edited as a whole — the forwarder sends a
// corrected packing list, not a diff — and line-by-line patching would need an
// identity for lines that the paperwork does not give them.
func (s *Service) UpdateShipment(ctx context.Context, tenantID, id int64, in ShipmentInput, op Operator) (ShipmentView, error) {
	lines, err := s.resolveShipmentLines(ctx, tenantID, in.Items)
	if err != nil {
		return ShipmentView{}, err
	}

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.LockShipment(ctx, store.LockShipmentParams{TenantID: tenantID, ID: id})
		if err == pgx.ErrNoRows {
			return apierr.NotFound("EX_SHIPMENT_NOT_FOUND", "出运单不存在")
		}
		if err != nil {
			return err
		}
		if locked.Status != "DRAFT" {
			return apierr.Invalid("EX_SHIPMENT_NOT_DRAFT", "已开船的出运单不能修改，如需更正请作废后重开").
				WithMeta("shipment_no", locked.ShipmentNo, "status", locked.Status)
		}
		n, err := q.UpdateShipmentHeader(ctx, store.UpdateShipmentHeaderParams{
			TenantID: tenantID, ID: id,
			VesselName: in.VesselName, VoyageNo: in.VoyageNo, BlNo: in.BlNo,
			ContainerNo: in.ContainerNo, PortOfDischarge: in.PortOfDischarge,
			Etd: in.ETD, Eta: in.ETA, Remark: in.Remark,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.Conflict("EX_SHIPMENT_STATUS_CHANGED", "出运单状态已变化，请刷新后重试")
		}
		if err := q.DeleteShipmentItems(ctx, store.DeleteShipmentItemsParams{
			TenantID: tenantID, ShipmentID: id,
		}); err != nil {
			return err
		}
		return insertShipmentLines(ctx, q, tenantID, id, lines)
	})
	if err != nil {
		return ShipmentView{}, err
	}
	return s.GetShipmentDoc(ctx, tenantID, id)
}

// ConfirmSailing is the moment the goods count as gone.
//
// It is the only place that writes into contract_shipments, which is what
// every progress view reads. Until it runs, a draft can say whatever it likes
// and no contract is affected; after it runs, the sales side sees the goods
// leave. That separation is why the over-shipment check lives here and is
// authoritative here: two drafts can each look fine on their own and together
// exceed the contract, and only one of them can be the one that goes too far.
func (s *Service) ConfirmSailing(ctx context.Context, tenantID, id int64, op Operator) (ShipmentView, error) {
	var owners []int64
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.LockShipment(ctx, store.LockShipmentParams{TenantID: tenantID, ID: id})
		if err == pgx.ErrNoRows {
			return apierr.NotFound("EX_SHIPMENT_NOT_FOUND", "出运单不存在")
		}
		if err != nil {
			return err
		}
		if locked.Status != "DRAFT" {
			return apierr.Invalid("EX_SHIPMENT_NOT_DRAFT", "只有草稿状态的出运单可以确认开船").
				WithMeta("shipment_no", locked.ShipmentNo, "status", locked.Status)
		}
		items, err := q.ItemsOfShipment(ctx, store.ItemsOfShipmentParams{
			TenantID: tenantID, ShipmentID: id,
		})
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return apierr.Invalid("EX_SHIPMENT_EMPTY", "出运单没有货物明细，不能开船")
		}
		contracts, err := q.ContractsOnShipment(ctx, store.ContractsOnShipmentParams{
			TenantID: tenantID, ShipmentID: id,
		})
		if err != nil {
			return err
		}
		for _, c := range contracts {
			if err := s.checkNotOverShipped(ctx, q, tenantID, c, items); err != nil {
				return err
			}
			owners = append(owners, c.SalesEmployeeID)
		}

		for _, it := range items {
			if _, err := q.RecordShipment(ctx, store.RecordShipmentParams{
				TenantID: tenantID, ContractID: it.ContractID,
				ContractItemID: it.ContractItemID, ProductID: it.ProductID,
				SkuID: it.SkuID, OutboundNo: locked.ShipmentNo, Qty: it.Qty,
			}); err != nil {
				return err
			}
		}
		n, err := q.SetShipmentStatus(ctx, store.SetShipmentStatusParams{
			TenantID: tenantID, ID: id, Status: "SHIPPED", FromStatus: "DRAFT",
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.Conflict("EX_SHIPMENT_STATUS_CHANGED", "出运单状态已变化，请刷新后重试")
		}
		return nil
	})
	if err != nil {
		return ShipmentView{}, err
	}
	s.tellOwners(ctx, tenantID, owners)
	return s.GetShipmentDoc(ctx, tenantID, id)
}

// MarkArrived closes the voyage. It touches no quantities: the goods left when
// the boat sailed, and arrival changes where they are, not whether they went.
func (s *Service) MarkArrived(ctx context.Context, tenantID, id int64, op Operator) (ShipmentView, error) {
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		n, err := q.SetShipmentStatus(ctx, store.SetShipmentStatusParams{
			TenantID: tenantID, ID: id, Status: "ARRIVED", FromStatus: "SHIPPED",
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.Invalid("EX_SHIPMENT_NOT_SHIPPED", "只有已开船的出运单可以标记到港")
		}
		return nil
	})
	if err != nil {
		return ShipmentView{}, err
	}
	return s.GetShipmentDoc(ctx, tenantID, id)
}

// CancelShipment voids a booking and, if it had already sailed on paper, takes
// its goods back out of the progress ledger.
//
// The document itself stays, marked CANCELLED. The ledger rows are derived
// data and can be rebuilt from the documents; the document is the record of
// what somebody entered and later withdrew, and deleting it would erase the
// fact that the mistake was made.
func (s *Service) CancelShipment(ctx context.Context, tenantID, id int64, op Operator) (ShipmentView, error) {
	var owners []int64
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.LockShipment(ctx, store.LockShipmentParams{TenantID: tenantID, ID: id})
		if err == pgx.ErrNoRows {
			return apierr.NotFound("EX_SHIPMENT_NOT_FOUND", "出运单不存在")
		}
		if err != nil {
			return err
		}
		switch locked.Status {
		case "CANCELLED":
			return apierr.Invalid("EX_SHIPMENT_CANCELLED", "出运单已作废")
		case "ARRIVED":
			return apierr.Invalid("EX_SHIPMENT_ARRIVED", "已到港的出运单不能作废")
		}
		contracts, err := q.ContractsOnShipment(ctx, store.ContractsOnShipmentParams{
			TenantID: tenantID, ShipmentID: id,
		})
		if err != nil {
			return err
		}
		for _, c := range contracts {
			owners = append(owners, c.SalesEmployeeID)
		}
		if _, err := q.RemoveShipmentFromLedger(ctx, store.RemoveShipmentFromLedgerParams{
			TenantID: tenantID, OutboundNo: locked.ShipmentNo,
		}); err != nil {
			return err
		}
		n, err := q.SetShipmentStatus(ctx, store.SetShipmentStatusParams{
			TenantID: tenantID, ID: id, Status: "CANCELLED", FromStatus: locked.Status,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.Conflict("EX_SHIPMENT_STATUS_CHANGED", "出运单状态已变化，请刷新后重试")
		}
		return nil
	})
	if err != nil {
		return ShipmentView{}, err
	}
	s.tellOwners(ctx, tenantID, owners)
	return s.GetShipmentDoc(ctx, tenantID, id)
}

func (s *Service) GetShipmentDoc(ctx context.Context, tenantID, id int64) (ShipmentView, error) {
	head, err := s.q.GetShipment(ctx, store.GetShipmentParams{TenantID: tenantID, ID: id})
	if err == pgx.ErrNoRows {
		return ShipmentView{}, apierr.NotFound("EX_SHIPMENT_NOT_FOUND", "出运单不存在")
	}
	if err != nil {
		return ShipmentView{}, err
	}
	items, err := s.q.ItemsOfShipment(ctx, store.ItemsOfShipmentParams{
		TenantID: tenantID, ShipmentID: id,
	})
	if err != nil {
		return ShipmentView{}, err
	}
	return ShipmentView{Shipment: head, Items: items}, nil
}

// ShipmentQuery is the list filter.
type ShipmentQuery struct {
	Status     string
	Keyword    string
	ContractID int64
	Page, Size int32
}

func (s *Service) ListShipments(ctx context.Context, tenantID int64, qy ShipmentQuery, op Operator) ([]store.ListShipmentsRow, int64, error) {
	visible, err := s.visibleTo(ctx, op)
	if err != nil {
		return nil, 0, err
	}
	page, size := normalizePage(qy.Page, qy.Size)
	rows, err := s.q.ListShipments(ctx, store.ListShipmentsParams{
		TenantID: tenantID,
		VisibleAll: visible.All, VisibleIds: visible.EmployeeIDs, OperatorID: op.ID,
		Status: qy.Status, Keyword: qy.Keyword, ContractID: qy.ContractID,
		RowLimit: size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, nil
}

// VesselsForContract answers "which boat is my customer's order on".
func (s *Service) VesselsForContract(ctx context.Context, tenantID, contractID int64) ([]store.VesselsForContractRow, error) {
	return s.q.VesselsForContract(ctx, store.VesselsForContractParams{
		TenantID: tenantID, ContractID: contractID,
	})
}

// resolvedLine is an input line with the contract's own words copied onto it.
type resolvedLine struct {
	in      ShipmentLineInput
	qty     decimal.Decimal
	item    store.ListContractItemsRow
	contract store.GetContractRow
}

// resolveShipmentLines turns ids into a checked, snapshotted packing list.
//
// Every line is read back from the contract's in-force version rather than
// trusted from the request: the product name and unit printed on a bill of
// lading have to be the ones on the contract, and a client that sends its own
// copy of them is one stale page away from shipping under the wrong
// description.
func (s *Service) resolveShipmentLines(ctx context.Context, tenantID int64, in []ShipmentLineInput) ([]resolvedLine, error) {
	if len(in) == 0 {
		return nil, apierr.Invalid("EX_SHIPMENT_ITEMS_REQUIRED", "请至少录入一条货物明细")
	}
	contracts := make(map[int64]store.GetContractRow)
	items := make(map[int64]map[int64]store.ListContractItemsRow)
	out := make([]resolvedLine, 0, len(in))
	seen := make(map[[2]int64]bool, len(in))

	for _, line := range in {
		qty, err := decimal.NewFromString(line.Qty)
		if err != nil || qty.LessThanOrEqual(decimal.Zero) {
			return nil, apierr.Invalid("EX_SHIPMENT_QTY_INVALID", "出运数量必须大于 0")
		}
		key := [2]int64{line.ContractID, line.ContractItemID}
		if seen[key] {
			return nil, apierr.Invalid("EX_SHIPMENT_LINE_DUPLICATE",
				"同一张出运单里同一条合同明细只能录一行，请把数量合并")
		}
		seen[key] = true

		contract, ok := contracts[line.ContractID]
		if !ok {
			row, err := s.q.GetContract(ctx, store.GetContractParams{
				TenantID: tenantID, ID: line.ContractID,
			})
			if err == pgx.ErrNoRows {
				return nil, apierr.NotFound("EX_CONTRACT_NOT_FOUND", "合同不存在")
			}
			if err != nil {
				return nil, err
			}
			if !shippableStatus(row.Status) {
				return nil, apierr.Invalid("EX_CONTRACT_NOT_SHIPPABLE",
					"合同尚未生效或已取消，不能安排出运").
					WithMeta("contract_no", row.ContractNo, "status", row.Status)
			}
			if row.CurrentVersionID == 0 {
				return nil, apierr.Invalid("EX_CONTRACT_NO_VERSION",
					"合同还没有生效版本，不能安排出运").
					WithMeta("contract_no", row.ContractNo)
			}
			contract = row
			contracts[line.ContractID] = row
		}

		byID, ok := items[line.ContractID]
		if !ok {
			rows, err := s.q.ListContractItems(ctx, store.ListContractItemsParams{
				TenantID: tenantID, ContractVersionID: contract.CurrentVersionID,
			})
			if err != nil {
				return nil, err
			}
			byID = make(map[int64]store.ListContractItemsRow, len(rows))
			for _, r := range rows {
				byID[r.ID] = r
			}
			items[line.ContractID] = byID
		}
		item, ok := byID[line.ContractItemID]
		if !ok {
			return nil, apierr.Invalid("EX_SHIPMENT_ITEM_NOT_ON_CONTRACT",
				"这条明细不属于该合同的生效版本，请重新选择").
				WithMeta("contract_no", contract.ContractNo)
		}
		out = append(out, resolvedLine{in: line, qty: qty, item: item, contract: contract})
	}
	return out, nil
}

func insertShipmentLines(ctx context.Context, q *store.Queries, tenantID, shipmentID int64, lines []resolvedLine) error {
	for i, l := range lines {
		if err := q.AddShipmentItem(ctx, store.AddShipmentItemParams{
			TenantID: tenantID, ShipmentID: shipmentID, LineNo: int32(i + 1),
			ContractID: l.contract.ID, ContractNo: l.contract.ContractNo,
			CustomerName: l.contract.CustomerName, ContractItemID: l.item.ID,
			ProductID: l.item.ProductID, SkuID: skuOf(l.item.SkuID),
			ProductCode: l.item.ProductCode, ProductName: l.item.ProductName,
			Spec: l.item.Spec, Qty: l.qty.String(), UomCode: l.item.UomCode,
		}); err != nil {
			return err
		}
	}
	return nil
}

// checkNotOverShipped refuses a sailing that would put more on the water than
// the contract sold.
//
// Grouped by product rather than by contract line, matching how progress is
// read: a contract change rewrites line ids, and a check keyed on them would
// let the same goods ship twice across a version boundary.
func (s *Service) checkNotOverShipped(
	ctx context.Context, q *store.Queries, tenantID int64,
	c store.ContractsOnShipmentRow, items []store.ItemsOfShipmentRow,
) error {
	if c.CurrentVersionID == 0 {
		return apierr.Invalid("EX_CONTRACT_NO_VERSION", "合同还没有生效版本，不能开船").
			WithMeta("contract_no", c.ContractNo)
	}
	progress, err := q.ShipmentProgressOf(ctx, store.ShipmentProgressOfParams{
		TenantID: tenantID, ContractID: c.ContractID, ContractVersionID: c.CurrentVersionID,
	})
	if err != nil {
		return err
	}
	type key struct{ product, sku int64 }
	remaining := make(map[key]decimal.Decimal, len(progress))
	names := make(map[key]string, len(progress))
	for _, p := range progress {
		k := key{p.ProductID, p.SkuID}
		remaining[k] = mustDec(p.RemainingQty)
		names[k] = p.ProductName
	}

	wanted := make(map[key]decimal.Decimal)
	for _, it := range items {
		if it.ContractID != c.ContractID {
			continue
		}
		k := key{it.ProductID, it.SkuID}
		wanted[k] = wanted[k].Add(mustDec(it.Qty))
	}
	for k, want := range wanted {
		left := remaining[k]
		if want.GreaterThan(left) {
			name := names[k]
			if name == "" {
				name = "该产品"
			}
			return apierr.Invalid("EX_SHIP_EXCEEDS_CONTRACT",
				"出运数量超过合同未发数量，请核对后修改").
				WithMeta(
					"contract_no", c.ContractNo,
					"product", name,
					"remaining", left.String(),
					"requested", want.String(),
					"over", want.Sub(left).String(),
				)
		}
	}
	return nil
}

// tellOwners nudges the salespeople whose contracts just moved. Best effort:
// a push that does not arrive costs a refresh, and is not worth failing a
// committed shipment over.
func (s *Service) tellOwners(ctx context.Context, tenantID int64, owners []int64) {
	if s.live == nil || len(owners) == 0 {
		return
	}
	seen := make(map[int64]bool, len(owners))
	uniq := make([]int64, 0, len(owners))
	for _, id := range owners {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return
	}
	s.live.ToEmployees(ctx, tenantID, uniq, livefeed.Event{
		Type: livefeed.DocChanged, Subject: "SHIPMENT",
	})
}

// shippableStatus: goods may go on a boat once the paper is in force. A draft
// or a contract still in approval has nothing agreed to ship against, and a
// cancelled one has nothing left to ship.
func shippableStatus(status string) bool {
	switch status {
	case "EFFECTIVE", "EXECUTING", "COMPLETED":
		return true
	}
	return false
}

// skuOf flattens a contract line's optional SKU. Shipment lines store 0 for
// "no SKU" so that grouping by product works without null-handling in every
// query that touches them.
func skuOf(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}

// mustDec reads a numeric that came back from the database as text. The
// database produced it, so a parse failure is a bug rather than bad input;
// zero keeps the caller's arithmetic well-defined either way.
func mustDec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}
