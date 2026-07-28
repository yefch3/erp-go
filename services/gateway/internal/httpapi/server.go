// Package httpapi is the REST face of the system: it validates JWTs, maps
// JSON onto gRPC calls, and translates gRPC statuses back into HTTP. It
// holds no business logic and no database.
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	apv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	fxv1 "github.com/sgao19/erp-go/gen/go/erp/fx/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/authtoken"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/livefeed"
)

type Server struct {
	IAM         iamv1.AuthServiceClient
	Directory   iamv1.DirectoryServiceClient
	Access      iamv1.AccessServiceClient
	Customers   mdv1.CustomerServiceClient
	Suppliers   mdv1.SupplierServiceClient
	Options     mdv1.OptionServiceClient
	Numbering   mdv1.NumberingServiceClient
	Fx          fxv1.FxServiceClient
	Approval    apv1.ApprovalServiceClient
	Catalog     pdv1.CatalogServiceClient
	Attachments pdv1.AttachmentServiceClient
	Quotations  exv1.QuotationServiceClient
	Contracts   exv1.ContractServiceClient
	Live        *livefeed.Subscriber
	JWTSecret   string
	Log         *slog.Logger
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	// Without this a handler panic drops the connection, which reaches the
	// user as "network error" instead of a readable failure.
	r.Use(s.recoverPanics)
	r.Post("/api/auth/login", s.login)
	r.Group(func(r chi.Router) {
		r.Use(s.auth)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers", s.listCustomers)
		r.With(s.perm("masterdata:customer:write")).Post("/api/customers", s.createCustomer)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers/{id}", s.getCustomer)
		r.With(s.perm("masterdata:customer:write")).Put("/api/customers/{id}", s.updateCustomer)
		r.With(s.perm("masterdata:customer:write")).Delete("/api/customers/{id}", s.deactivateCustomer)
		r.With(s.perm("masterdata:customer:write")).Post("/api/customers/{id}/activate", s.activateCustomer)
		r.With(s.perm("masterdata:supplier:read")).Get("/api/suppliers", s.listSuppliers)
		r.With(s.perm("masterdata:supplier:write")).Post("/api/suppliers", s.createSupplier)
		// Option dictionaries feed every form's dropdowns; login is enough.
		r.Get("/api/options", s.listOptions)
		r.Post("/api/numbering/next", s.nextNumber)
		// Organisation and access control. Reading the directory is what every
		// picker needs; changing it is administrator work.
		r.With(s.perm("iam:employee:read")).Get("/api/departments", s.listDepartments)
		r.With(s.perm("iam:employee:write")).Post("/api/departments", s.createDepartment)
		r.With(s.perm("iam:employee:read")).Get("/api/employees", s.listEmployees)
		r.With(s.perm("iam:employee:read")).Get("/api/employees/{id}", s.getEmployee)
		r.With(s.perm("iam:employee:write")).Post("/api/employees", s.createEmployee)
		r.With(s.perm("iam:employee:write")).Delete("/api/employees/{id}", s.deactivateEmployee)
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/activate", s.activateEmployee)
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/account", s.openAccount)
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/password", s.resetPassword)
		r.With(s.perm("iam:role:write")).Post("/api/employees/{id}/roles", s.assignRoles)
		// The reporting line is org-chart maintenance, not access control,
		// so it sits with the rest of employee editing.
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/manager", s.setManager)
		r.With(s.perm("iam:role:read")).Get("/api/roles", s.listRoles)
		r.With(s.perm("iam:role:write")).Post("/api/roles", s.createRole)
		r.With(s.perm("iam:role:write")).Put("/api/roles/{id}/permissions", s.grantRolePermissions)
		r.With(s.perm("iam:role:read")).Get("/api/roles/{id}/members", s.listRoleMembers)
		r.With(s.perm("iam:role:read")).Get("/api/permissions", s.listPermissions)
		// Data scope: which documents a role may see, as opposed to which
		// features it may use. Same administrator, different question.
		r.With(s.perm("iam:role:read")).Get("/api/data-scopes", s.listDataScopes)
		r.With(s.perm("iam:role:write")).Put("/api/roles/{id}/data-scope", s.setDataScope)
		// Approval flow design. Saving always writes a new version, so this
		// is a write on the flow rather than on any running approval.
		r.With(s.perm("approval:flow:read")).Get("/api/approval-flows", s.listFlows)
		r.With(s.perm("approval:flow:read")).Get("/api/approval-flows/{id}", s.getFlow)
		r.With(s.perm("approval:flow:write")).Post("/api/approval-flows", s.saveFlow)
		r.With(s.perm("approval:flow:write")).Delete("/api/approval-flows/band", s.deleteFlowBand)
		// Own identity and own password: being logged in is the only
		// requirement, since neither can touch anybody else's account.
		r.Get("/api/me/permissions", s.me)
		// The live feed carries only "go and re-read X" hints, so being
		// logged in is the whole requirement; every actual read still goes
		// through its own permission-checked route.
		r.Get("/api/events", s.streamEvents)
		r.Post("/api/me/password", s.changeOwnPassword)
		// Product catalog. Categories and units are reference data every
		// product form needs, so reading them only requires product:read.
		r.With(s.perm("product:product:read")).Get("/api/product-categories", s.listCategories)
		r.With(s.perm("product:product:write")).Post("/api/product-categories", s.createCategory)
		r.With(s.perm("product:product:write")).Put("/api/product-categories/{id}", s.updateCategory)
		r.With(s.perm("product:product:write")).Delete("/api/product-categories/{id}", s.deactivateCategory)
		r.With(s.perm("product:product:read")).Get("/api/uoms", s.listUoms)
		r.With(s.perm("product:product:write")).Post("/api/uoms", s.createUom)
		r.With(s.perm("product:product:read")).Get("/api/products", s.listProducts)
		r.With(s.perm("product:product:read")).Get("/api/products/{id}", s.getProduct)
		r.With(s.perm("product:product:write")).Post("/api/products", s.createProduct)
		r.With(s.perm("product:product:write")).Put("/api/products/{id}", s.updateProduct)
		r.With(s.perm("product:product:write")).Delete("/api/products/{id}", s.deactivateProduct)
		r.With(s.perm("product:product:write")).Post("/api/products/{id}/activate", s.activateProduct)
		r.With(s.perm("product:product:read")).Get("/api/products/{id}/skus", s.listSkus)
		r.With(s.perm("product:product:write")).Post("/api/products/{id}/skus", s.createSku)
		r.With(s.perm("product:product:write")).Delete("/api/skus/{id}", s.deactivateSku)
		r.With(s.perm("product:product:read")).Get("/api/products/{id}/attachments", s.listAttachments)
		r.With(s.perm("product:product:write")).Post("/api/products/{id}/attachments/presign", s.presignUpload)
		r.With(s.perm("product:product:write")).Post("/api/products/{id}/attachments", s.registerAttachment)
		r.With(s.perm("product:product:write")).Delete("/api/attachments/{id}", s.removeAttachment)
		// Quotations. Sending and recording the customer's answer are writes:
		// they change what the company has promised.
		r.With(s.perm("export:quotation:read")).Get("/api/quotations", s.listQuotations)
		r.With(s.perm("export:quotation:read")).Get("/api/quotations/{id}", s.getQuotation)
		r.With(s.perm("export:quotation:write")).Post("/api/quotations", s.createQuotation)
		r.With(s.perm("export:quotation:write")).Put("/api/quotations/{id}", s.updateQuotation)
		r.With(s.perm("export:quotation:write")).Post("/api/quotations/{id}/send", s.sendQuotation)
		r.With(s.perm("export:quotation:write")).Post("/api/quotations/{id}/respond", s.respondQuotation)
		r.With(s.perm("export:quotation:write")).Post("/api/quotations/{id}/cancel", s.cancelQuotation)
		// Contracts. Signing is what makes a version binding, so it sits
		// behind the write permission like every other state change.
		r.With(s.perm("export:contract:read")).Get("/api/contracts", s.listContracts)
		r.With(s.perm("export:contract:read")).Get("/api/contracts/{id}", s.getContract)
		r.With(s.perm("export:contract:write")).Post("/api/contracts", s.createContract)
		r.With(s.perm("export:contract:write")).Put("/api/contracts/{id}", s.updateContract)
		r.With(s.perm("export:contract:write")).Post("/api/contracts/{id}/submit", s.submitContract)
		r.With(s.perm("export:contract:write")).Post("/api/contracts/{id}/change", s.changeContract)
		r.With(s.perm("export:contract:write")).Post("/api/contracts/{id}/sign", s.signContract)
		r.With(s.perm("export:contract:write")).Post("/api/contracts/{id}/cancel", s.cancelContract)
		// Contract paperwork. The bytes never pass through here: the browser
		// uploads straight to object storage with a signed URL.
		r.With(s.perm("export:contract:read")).Get("/api/contracts/{id}/files", s.listContractFiles)
		r.With(s.perm("export:contract:write")).Post("/api/contracts/{id}/files/presign", s.presignContractFile)
		r.With(s.perm("export:contract:write")).Post("/api/contracts/{id}/files", s.registerContractFile)
		r.With(s.perm("export:contract:write")).Delete("/api/contract-files/{id}", s.removeContractFile)
		// Handing a deal to somebody else is a supervisor's act, so it gets its
		// own permission rather than riding on :write — the people who may edit
		// their own documents are exactly the people who may not reassign them.
		r.With(s.perm("export:ownership:transfer")).Post("/api/ownership/transfer", s.transferOwnership)
		// Reading the handover history is scoped like reading the document, so
		// the contract's own permission is the right gate.
		r.With(s.perm("export:contract:read")).Get("/api/ownership/transfers", s.listOwnershipTransfers)
		// Approval todos are personal: the service filters by the caller's
		// employee id, so the permission only gates "may act on approvals".
		r.With(s.perm("approval:task:act")).Get("/api/approvals/todos", s.myTodos)
		r.With(s.perm("approval:task:act")).Post("/api/approvals/tasks/{id}/act", s.actOnTask)
		// Reading where a document stands is not acting on it: the salesperson
		// who submitted a contract needs to see it is waiting on the sales
		// manager without any power to approve anything.
		r.With(s.perm("approval:instance:read")).Get("/api/approvals/instances", s.listApprovalInstances)
		r.With(s.perm("approval:instance:read")).Get("/api/approvals/instances/{id}", s.getApprovalInstance)
		r.With(s.perm("fx:rate:read")).Get("/api/fx/latest", s.fxLatest)
		r.With(s.perm("fx:rate:read")).Get("/api/fx/rates", s.fxRates)
		r.With(s.perm("fx:rate:read")).Get("/api/fx/anomalies", s.fxAnomalies)
	})
	return r
}

// perm enforces a permission code server-side by asking iam. The frontend
// hides controls for UX; this is the actual security boundary.
func (s *Server) perm(code string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			op, ok := grpcx.OperatorFromContext(r.Context())
			if !ok || op.EmployeeID == 0 {
				s.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_MISSING", "缺少登录凭证")
				return
			}
			resp, err := s.Access.CheckPermission(r.Context(), &iamv1.CheckPermissionRequest{
				EmployeeId: op.EmployeeID, PermissionCode: code,
			})
			if err != nil {
				s.writeGRPCError(w, err)
				return
			}
			if !resp.GetAllowed() {
				s.writeError(w, http.StatusForbidden, "AUTH_PERMISSION_DENIED", "没有执行此操作的权限")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------- middleware

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.Log.Error("panic in handler", "path", r.URL.Path, "panic", v)
				s.writeError(w, http.StatusInternalServerError, "GATEWAY_PANIC", "服务内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// auth validates the Bearer token and turns its claims into the operator
// context; the gRPC client interceptor then forwards them as metadata so
// downstream services see who is acting.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if raw == "" || raw == r.Header.Get("Authorization") {
			s.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_MISSING", "缺少登录凭证")
			return
		}
		claims, err := authtoken.Parse(s.JWTSecret, raw)
		if err != nil {
			s.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "登录凭证无效或已过期")
			return
		}
		ctx := grpcx.WithOperator(r.Context(), grpcx.Operator{
			TenantID:   claims.TenantID,
			EmployeeID: claims.EmployeeID(),
			Name:       claims.EmployeeName,
			IP:         r.RemoteAddr,
			TraceID:    newTraceID(),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newTraceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------- responses

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
}

var pj = protojson.MarshalOptions{EmitUnpopulated: true}

func (s *Server) writeProto(w http.ResponseWriter, msg proto.Message) {
	data, err := pj.Marshal(msg)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "GATEWAY_MARSHAL", "响应编码失败")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{Success: true, Data: data})
}

func (s *Server) writeError(w http.ResponseWriter, httpStatus int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(envelope{Success: false, Code: code, Message: msg})
}

// writeGRPCError maps a gRPC failure onto HTTP, keeping the stable business
// code from apierr so the frontend can branch on it.
func (s *Server) writeGRPCError(w http.ResponseWriter, err error) {
	st := status.Convert(err)
	httpCode := map[codes.Code]int{
		codes.InvalidArgument:    http.StatusBadRequest,
		codes.NotFound:           http.StatusNotFound,
		codes.FailedPrecondition: http.StatusConflict,
		codes.PermissionDenied:   http.StatusForbidden,
		codes.Unauthenticated:    http.StatusUnauthorized,
	}[st.Code()]
	if httpCode == 0 {
		httpCode = http.StatusInternalServerError
	}
	bizCode := apierr.CodeFromStatus(err)
	if bizCode == "" {
		bizCode = "INTERNAL"
	}
	s.writeError(w, httpCode, bizCode, st.Message())
}

// decodeBody parses a JSON request body directly into the gRPC request
// message, so REST and gRPC share one schema definition (the proto).
func (s *Server) decodeBody(w http.ResponseWriter, r *http.Request, msg proto.Message) bool {
	body, ok := s.readBody(w, r)
	if !ok {
		return false
	}
	if err := protojson.Unmarshal(body, msg); err != nil {
		s.writeError(w, http.StatusBadRequest, "GATEWAY_BAD_JSON", "请求体不是合法的 JSON："+err.Error())
		return false
	}
	return true
}

// decodeJSON is for the few endpoints whose REST body is not a proto message,
// e.g. an enum expressed as a plain word.
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	body, ok := s.readBody(w, r)
	if !ok {
		return false
	}
	if err := json.Unmarshal(body, target); err != nil {
		s.writeError(w, http.StatusBadRequest, "GATEWAY_BAD_JSON", "请求体不是合法的 JSON："+err.Error())
		return false
	}
	return true
}

func (s *Server) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		body = append(body, buf[:n]...)
		if err != nil {
			break
		}
		if len(body) > 1<<20 {
			s.writeError(w, http.StatusRequestEntityTooLarge, "GATEWAY_BODY_TOO_LARGE", "请求体过大")
			return nil, false
		}
	}
	return body, true
}
