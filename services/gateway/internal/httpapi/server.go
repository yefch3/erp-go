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

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	fxv1 "github.com/sgao19/erp-go/gen/go/erp/fx/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/authtoken"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

type Server struct {
	IAM        iamv1.AuthServiceClient
	Access     iamv1.AccessServiceClient
	Customers  mdv1.CustomerServiceClient
	Suppliers  mdv1.SupplierServiceClient
	Options    mdv1.OptionServiceClient
	Numbering  mdv1.NumberingServiceClient
	Fx         fxv1.FxServiceClient
	JWTSecret  string
	Log        *slog.Logger
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Post("/api/auth/login", s.login)
	r.Group(func(r chi.Router) {
		r.Use(s.auth)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers", s.listCustomers)
		r.With(s.perm("masterdata:customer:write")).Post("/api/customers", s.createCustomer)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers/{id}", s.getCustomer)
		r.With(s.perm("masterdata:customer:write")).Put("/api/customers/{id}", s.updateCustomer)
		r.With(s.perm("masterdata:customer:write")).Delete("/api/customers/{id}", s.deactivateCustomer)
		r.With(s.perm("masterdata:supplier:read")).Get("/api/suppliers", s.listSuppliers)
		r.With(s.perm("masterdata:supplier:write")).Post("/api/suppliers", s.createSupplier)
		// Option dictionaries feed every form's dropdowns; login is enough.
		r.Get("/api/options", s.listOptions)
		r.Post("/api/numbering/next", s.nextNumber)
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
			return false
		}
	}
	if err := protojson.Unmarshal(body, msg); err != nil {
		s.writeError(w, http.StatusBadRequest, "GATEWAY_BAD_JSON", "请求体不是合法的 JSON："+err.Error())
		return false
	}
	return true
}
