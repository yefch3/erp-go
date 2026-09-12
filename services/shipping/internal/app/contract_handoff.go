package app

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

type EventClaim func(context.Context, pgx.Tx) error

type ContractEffective struct {
	ContractID   int64                       `json:"contract_id"`
	ContractNo   string                      `json:"contract_no"`
	VersionID    int64                       `json:"version_id"`
	VersionNo    int32                       `json:"version_no"`
	CustomerID   int64                       `json:"customer_id"`
	CustomerName string                      `json:"customer_name"`
	Shipments    []ContractEffectiveShipment `json:"shipments"`
	Items        []ContractEffectiveItem     `json:"items"`
}

type ContractEffectiveItem struct {
	LineNo         int32  `json:"line_no"`
	ContractItemID int64  `json:"contract_item_id"`
	ProductCode    string `json:"product_code"`
	ProductName    string `json:"product_name"`
	Spec           string `json:"spec"`
	Qty            string `json:"qty"`
	UomCode        string `json:"uom_code"`
	Remark         string `json:"remark"`
}

type ContractEffectiveShipment struct {
	BatchNo            int32  `json:"batch_no"`
	ShipmentGroupKey   string `json:"shipment_group_key"`
	CarrierForwarder   string `json:"carrier_forwarder"`
	ServiceOptionName  string `json:"service_option_name"`
	CustomerManaged    bool   `json:"customer_managed"`
	Currency           string `json:"currency"`
	FreightAmount      string `json:"freight_amount"`
	ChargeBasis        string `json:"charge_basis"`
	PortOfLoading      string `json:"port_of_loading"`
	PortOfDischarge    string `json:"port_of_discharge"`
	EstimatedDeparture string `json:"estimated_departure"`
	EstimatedArrival   string `json:"estimated_arrival"`
	ValidUntil         string `json:"valid_until"`
	Remark             string `json:"remark"`
}

func (s *Service) HandoffsFromContract(ctx context.Context, tenantID int64, event ContractEffective, claim EventClaim) error {
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := claim(ctx, tx); err != nil {
			return err
		}
		q := s.q.WithTx(tx)
		for _, shipment := range event.Shipments {
			if err := q.UpsertContractShippingHandoff(ctx, store.UpsertContractShippingHandoffParams{
				TenantID: tenantID, ContractID: event.ContractID, ContractNo: event.ContractNo,
				ContractVersionID: event.VersionID, VersionNo: event.VersionNo, CustomerID: event.CustomerID,
				CustomerName: event.CustomerName, BatchNo: shipment.BatchNo, ShipmentGroupKey: shipment.ShipmentGroupKey,
				CarrierForwarder: shipment.CarrierForwarder, ServiceOptionName: shipment.ServiceOptionName,
				CustomerManaged: shipment.CustomerManaged, Currency: shipment.Currency, FreightAmount: shipment.FreightAmount,
				ChargeBasis: shipment.ChargeBasis, PortOfLoading: shipment.PortOfLoading, PortOfDischarge: shipment.PortOfDischarge,
				EstimatedDeparture: shipment.EstimatedDeparture, EstimatedArrival: shipment.EstimatedArrival,
				ValidUntil: shipment.ValidUntil, Remark: shipment.Remark,
				InitialStatus: "WAITING_REQUOTE",
			}); err != nil {
				return err
			}
			var handoffID int64
			if err := tx.QueryRow(ctx, `SELECT id FROM contract_shipping_handoffs WHERE tenant_id=$1 AND contract_version_id=$2 AND batch_no=$3`, tenantID, event.VersionID, shipment.BatchNo).Scan(&handoffID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM contract_shipping_handoff_cargo WHERE tenant_id=$1 AND handoff_id=$2`, tenantID, handoffID); err != nil {
				return err
			}
			for _, item := range event.Items {
				if _, err := tx.Exec(ctx, `INSERT INTO contract_shipping_handoff_cargo (tenant_id,handoff_id,contract_item_id,line_no,product_code,product_name,specification,quantity,uom_code,remark) VALUES($1,$2,$3,$4,$5,$6,$7,$8::numeric,$9,$10)`, tenantID, handoffID, item.ContractItemID, item.LineNo, item.ProductCode, item.ProductName, item.Spec, item.Qty, item.UomCode, item.Remark); err != nil {
					return err
				}
			}
		}
		return q.SupersedeOldContractShippingHandoffs(ctx, store.SupersedeOldContractShippingHandoffsParams{
			TenantID: tenantID, ContractID: event.ContractID, ContractVersionID: event.VersionID,
		})
	})
}

func (s *Service) ListContractShippingHandoffs(ctx context.Context, tenantID int64, status string) ([]store.ListContractShippingHandoffsRow, error) {
	return s.q.ListContractShippingHandoffs(ctx, store.ListContractShippingHandoffsParams{TenantID: tenantID, Status: status})
}
