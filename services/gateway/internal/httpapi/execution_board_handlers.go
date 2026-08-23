package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
)

// 合同执行进程一览（D2）。
//
// 「这单到哪了」原来没有一个人答得上来：合同金额在出口合同页，订没订货在
// 采购页，货走没走在出运页，钱收没收在银行流水页。四个页面、四个部门，
// 谁都只看得见自己那一段。
//
// 三个服务各存各的，所以这一行只能在网关拼。**按页批量取，不按行**：出口
// 先出这一页的合同（分页和数据范围都由它定），再拿这一页的合同号去问采购
// 和船期——一页两次调用，20 行也是两次。
//
// 一条腿取不回来时不装作是零。零在这张表上的意思是「还没开始采购」，
// 和「采购服务连不上」是完全不同的两件事，读的人会照着做出完全不同的决定。
// 所以取不回来就明说取不回来，页面把那一列打灰。

type executionProcurement struct {
	TotalLines    int32 `json:"totalLines"`
	OrderedLines  int32 `json:"orderedLines"`
	ReceivedLines int32 `json:"receivedLines"`
}

type executionShipping struct {
	ScheduleID int64  `json:"scheduleId,string"`
	ScheduleNo string `json:"scheduleNo"`
	VesselName string `json:"vesselName"`
	VoyageNo   string `json:"voyageNo"`
	Status     string `json:"status"`
	ETD        string `json:"etd"`
	ATD        string `json:"atd"`
	ETA        string `json:"eta"`
	ATA        string `json:"ata"`
	DelayDays  int32  `json:"delayDays"`
	LegCount   int32  `json:"legCount"`
}

type executionRow struct {
	ContractID      int64  `json:"contractId,string"`
	ContractNo      string `json:"contractNo"`
	CustomerID      int64  `json:"customerId,string"`
	CustomerName    string `json:"customerName"`
	SalesEmployeeID int64  `json:"salesEmployeeId,string"`
	SalesEmployee   string `json:"salesEmployee"`
	Status          string `json:"status"`
	EffectiveDate   string `json:"effectiveDate"`
	Currency        string `json:"currency"`
	TotalAmount     string `json:"totalAmount"`
	ShippedAmount   string `json:"shippedAmount"`
	ReceivedAmount  string `json:"receivedAmount"`
	DueDate         string `json:"dueDate"`
	OverdueDays     int32  `json:"overdueDays"`
	DueUnset        bool   `json:"dueUnset"`
	// 没有采购需求的合同给零值：「一项都没有」和「还没开始采购」是同一件事。
	Procurement executionProcurement `json:"procurement"`
	// 还没订舱时为 null——这里没有「零班船」这种中间状态。
	Shipping *executionShipping `json:"shipping"`
}

type executionBoardResponse struct {
	Rows []executionRow `json:"rows"`
	Meta struct {
		Total int64 `json:"total,string"`
	} `json:"meta"`
	// 两条外腿各自取回来了没有。false 时对应的列在页面上打灰并注明取不到，
	// 而不是显示成零。
	ProcurementAvailable bool `json:"procurementAvailable"`
	ShippingAvailable    bool `json:"shippingAvailable"`
}

func (s *Server) listContractExecution(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	customerID, _ := strconv.ParseInt(q.Get("customer_id"), 10, 64)
	// 出口这一步定下这一页是哪些合同：分页、排序、数据范围都在它那里。
	// 它失败就整页失败——没有合同就没有行可拼。
	base, err := s.Contracts.ListContractExecution(r.Context(), &exv1.ListContractExecutionRequest{
		Status:     q.Get("status"),
		CustomerId: customerID,
		Keyword:    q.Get("keyword"),
		Page:       pageFromQuery(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}

	ids := make([]int64, 0, len(base.GetRows()))
	nos := make([]string, 0, len(base.GetRows()))
	for _, row := range base.GetRows() {
		ids = append(ids, row.GetContractId())
		if no := row.GetContractNo(); no != "" {
			nos = append(nos, no)
		}
	}

	var (
		wg          sync.WaitGroup
		procurement map[int64]*prv1.ContractProcurementProgressItem
		shipping    map[string]*shippingv1.ContractShippingSnapshotItem
		procOK      = true
		shipOK      = true
	)
	if len(ids) > 0 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			resp, callErr := s.Requirements.ContractProcurementProgress(r.Context(),
				&prv1.ContractProcurementProgressRequest{ContractIds: ids})
			if callErr != nil {
				procOK = false
				return
			}
			procurement = make(map[int64]*prv1.ContractProcurementProgressItem, len(resp.GetItems()))
			for _, item := range resp.GetItems() {
				procurement[item.GetContractId()] = item
			}
		}()
		go func() {
			defer wg.Done()
			if len(nos) == 0 {
				return
			}
			resp, callErr := s.Shipping.ContractShippingSnapshot(r.Context(),
				&shippingv1.ContractShippingSnapshotRequest{ContractNos: nos})
			if callErr != nil {
				shipOK = false
				return
			}
			shipping = make(map[string]*shippingv1.ContractShippingSnapshotItem, len(resp.GetItems()))
			for _, item := range resp.GetItems() {
				shipping[item.GetContractNo()] = item
			}
		}()
		wg.Wait()
	}

	out := executionBoardResponse{
		Rows:                 make([]executionRow, 0, len(base.GetRows())),
		ProcurementAvailable: procOK,
		ShippingAvailable:    shipOK,
	}
	out.Meta.Total = base.GetMeta().GetTotal()
	for _, row := range base.GetRows() {
		merged := executionRow{
			ContractID: row.GetContractId(), ContractNo: row.GetContractNo(),
			CustomerID: row.GetCustomerId(), CustomerName: row.GetCustomerName(),
			SalesEmployeeID: row.GetSalesEmployeeId(), SalesEmployee: row.GetSalesEmployee(),
			Status: row.GetStatus(), EffectiveDate: row.GetEffectiveDate(),
			Currency: row.GetCurrency(), TotalAmount: row.GetTotalAmount(),
			ShippedAmount: row.GetShippedAmount(), ReceivedAmount: row.GetReceivedAmount(),
			DueDate: row.GetDueDate(), OverdueDays: row.GetOverdueDays(), DueUnset: row.GetDueUnset(),
		}
		if p, ok := procurement[row.GetContractId()]; ok {
			merged.Procurement = executionProcurement{
				TotalLines: p.GetTotalLines(), OrderedLines: p.GetOrderedLines(),
				ReceivedLines: p.GetReceivedLines(),
			}
		}
		if sh, ok := shipping[row.GetContractNo()]; ok {
			merged.Shipping = &executionShipping{
				ScheduleID: sh.GetScheduleId(), ScheduleNo: sh.GetScheduleNo(),
				VesselName: sh.GetVesselName(), VoyageNo: sh.GetVoyageNo(),
				Status: sh.GetStatus(), ETD: sh.GetEtd(), ATD: sh.GetAtd(),
				ETA: sh.GetEta(), ATA: sh.GetAta(),
				DelayDays: sh.GetDelayDays(), LegCount: sh.GetLegCount(),
			}
		}
		out.Rows = append(out.Rows, merged)
	}
	s.writeJSON(w, out)
}

// writeJSON 用于网关自己拼出来的响应——一览表把三个服务的答复合成一行，
// 没有哪一个 proto 消息装得下它，也不该为此让某个服务的接口去认识另外
// 两个服务。
func (s *Server) writeJSON(w http.ResponseWriter, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "GATEWAY_MARSHAL", "响应编码失败")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{Success: true, Data: data})
}
