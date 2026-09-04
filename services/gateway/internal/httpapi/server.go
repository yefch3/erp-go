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
	"time"

	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	apv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	fxv1 "github.com/sgao19/erp-go/gen/go/erp/fx/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	ivv1 "github.com/sgao19/erp-go/gen/go/erp/inventory/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/authtoken"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/livefeed"
)

type Server struct {
	IAM       iamv1.AuthServiceClient
	Directory iamv1.DirectoryServiceClient
	Platform  iamv1.PlatformServiceClient
	Access    iamv1.AccessServiceClient
	Customers mdv1.CustomerServiceClient
	Suppliers mdv1.SupplierServiceClient
	// 信用评级（E3）：客户和供应商共用一套。
	CreditRatings mdv1.CreditRatingServiceClient
	Ports         mdv1.PortServiceClient
	Options       mdv1.OptionServiceClient
	Numbering     mdv1.NumberingServiceClient
	Fx            fxv1.FxServiceClient
	Approval      apv1.ApprovalServiceClient
	Catalog       pdv1.CatalogServiceClient
	Attachments   pdv1.AttachmentServiceClient
	Attributes    pdv1.AttributeServiceClient
	Quotations    exv1.QuotationServiceClient
	Contracts     exv1.ContractServiceClient
	Shipments     exv1.ShipmentServiceClient
	Receipts      exv1.ReceiptServiceClient
	Requirements  prv1.RequirementServiceClient
	Orders        prv1.PurchaseOrderServiceClient
	Sourcing      prv1.SourcingServiceClient
	// 询盘列模板：邮件标准化与待复核解析共用的列注册表。
	InquiryTemplates prv1.InquiryTemplateServiceClient
	Stocks           ivv1.StockServiceClient
	Shipping         shippingv1.ShippingServiceClient
	Emails           mailv1.EmailServiceClient
	// Unlock holds mailbox-verification tokens. Nil fails closed: every mail
	// route answers MAIL_LOCKED until a store exists.
	Unlock *UnlockStore
	// Idem 是 HTTP 写入的防重存储（见 idempotency.go）。Nil 时中间件原样
	// 放行——测试里不接 Redis 也能跑，但生产 main 必须接上。
	Idem *IdemStore
	// Throttle counts failed logins and failed mailbox verifications. Nil is
	// allowed and fails open — see FailureThrottle for why this one is the
	// other way round from Unlock.
	Throttle *FailureThrottle
	// Revocations ends one person's sessions without ending everybody's. Nil
	// disables the check: without it the only way to take a token back is to
	// rotate JWT_SECRET, which signs out the whole company.
	Revocations *RevocationStore
	// Limits bounds what one person or one source can cost: presses of 立即收信,
	// and hits on the two routes strangers are meant to reach. Nil allows
	// everything — see RateLimiter for why this one fails open.
	Limits *RateLimiter
	// Google OAuth. The client id is public by design; the secret lives only
	// in the notification service, which does the token exchange.
	GoogleClientID   string
	OAuthRedirectURL string
	FrontendBaseURL  string
	// Employee whose configured mailbox is the procurement shared identity.
	// Zero disables direct RFQ sending while leaving workbook download usable.
	ProcurementMailSenderID int64
	// TrustProxyHeaders says a reverse proxy sits in front and overwrites
	// X-Forwarded-For. Off by default: the header is caller-supplied, and
	// trusting it without such a proxy lets anybody spray from a different
	// fake address on every request. See clientAddr.
	TrustProxyHeaders bool
	// CookieSecure marks the session cookies HTTPS-only. Off for localhost
	// development; DEPLOY.md requires COOKIE_SECURE=1 wherever nginx
	// terminates TLS — a Secure-less cookie on a public host would ride any
	// accidental http:// request in the clear.
	CookieSecure bool
	Live         *livefeed.Subscriber
	JWTSecret    string
	// TokenTTL is the life of a renewed token, and must match the one iam
	// issues with. Because renewal rides on activity, this is in practice how
	// long somebody may sit idle before being signed out — not how long since
	// they signed in.
	TokenTTL time.Duration
	Log      *slog.Logger
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	// Without this a handler panic drops the connection, which reaches the
	// user as "network error" instead of a readable failure.
	r.Use(s.recoverPanics)
	// The deploy script's question and the monitor's question are the same
	// question: is the gateway process up and answering. Login-free because
	// both askers are machines with no session, and the answer — a version
	// string that is public in every response header of the frontend anyway —
	// gives an attacker nothing.
	r.Get("/api/healthz", s.healthz)
	r.Post("/api/auth/login", s.login)
	r.Post("/api/auth/logout", s.logout)
	// Activation. Login-free of necessity: whoever holds the link has no
	// account yet, and getting one is the point. The token in it is the whole
	// of their claim — see activateAccount for why that is enough.
	r.Get("/api/auth/invitation", s.peekInvitation)
	r.Post("/api/auth/activate", s.activateAccount)
	// Images embedded in sent mail. Public by necessity: the fetcher is the
	// recipient's mail client, which has no session. See serveMailImage.
	r.Get("/api/public/mail-images/{token}",
		s.limitPublic("img", publicImageBudget, s.serveMailImage))
	// The open-tracking pixel. Also login-free, and also deliberately
	// indistinguishable between a real key and a made-up one.
	r.Get("/api/public/mail-open/{key}",
		s.limitPublic("pixel", publicPixelBudget, s.serveOpenPixel))
	// Google sends the browser back here after its own login page. State is
	// the authentication; see googleOAuthCallback.
	r.Get("/api/oauth/google/callback", s.googleOAuthCallback)
	// Password reset. Login-free of necessity — the person asking cannot log
	// in — and rate-limited hard: these routes send mail and answer strangers.
	r.Post("/api/auth/forgot-password",
		s.limitPublic("reset", publicResetBudget, s.forgotPassword))
	r.Get("/api/auth/reset",
		s.limitPublic("reset", publicResetBudget, s.peekPasswordReset))
	r.Post("/api/auth/reset",
		s.limitPublic("reset", publicResetBudget, s.redeemPasswordReset))
	r.Group(func(r chi.Router) {
		r.Use(s.auth)
		// 挂在认证之后：防重的键按「哪家公司的哪个人」隔离，身份得先有。
		r.Use(s.idempotent)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers", s.listCustomers)
		r.With(s.perm("masterdata:customer:write")).Post("/api/customers", s.createCustomer)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers/countries", s.listCustomerCountryGroups)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers/duplicates", s.checkCustomerDuplicates)
		r.With(s.perm("masterdata:customer:write")).Post("/api/customers/import", s.importCustomers)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers/{id}", s.getCustomer)
		// 信用评级（E3）。读跟着各自主数据的读权限走；打分要写权限——
		// 评级是对外授信和下单的依据，不是备注。
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers/{id}/credit-ratings", s.listCustomerCreditRatings)
		r.With(s.perm("masterdata:customer:write")).Post("/api/customers/{id}/credit-ratings", s.rateCustomerCredit)
		r.With(s.perm("masterdata:supplier:read")).Get("/api/suppliers/{id}/credit-ratings", s.listSupplierCreditRatings)
		r.With(s.perm("masterdata:supplier:write")).Post("/api/suppliers/{id}/credit-ratings", s.rateSupplierCredit)
		r.With(s.perm("masterdata:customer:write")).Put("/api/customers/{id}", s.updateCustomer)
		r.With(s.perm("masterdata:customer:write")).Put("/api/customers/{id}/profile", s.updateCustomerProfile)
		r.With(s.perm("masterdata:customer:write")).Delete("/api/customers/{id}", s.deactivateCustomer)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers/{id}/deactivation-impact", s.getCustomerDeactivationImpact)
		r.With(s.perm("masterdata:customer:write")).Post("/api/customers/{id}/activate", s.activateCustomer)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers/{id}/addresses", s.listCustomerAddresses)
		r.With(s.perm("masterdata:customer:write")).Post("/api/customers/{id}/addresses", s.createCustomerAddress)
		r.With(s.perm("masterdata:customer:write")).Put("/api/customers/{id}/addresses/{addressId}", s.updateCustomerAddress)
		r.With(s.perm("masterdata:customer:write")).Delete("/api/customers/{id}/addresses/{addressId}", s.deactivateCustomerAddress)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers/{id}/contacts", s.listCustomerContacts)
		r.With(s.perm("masterdata:customer:write")).Post("/api/customers/{id}/contacts", s.createCustomerContact)
		r.With(s.perm("masterdata:customer:write")).Put("/api/customers/{id}/contacts/{contactId}", s.updateCustomerContact)
		r.With(s.perm("masterdata:customer:write")).Delete("/api/customers/{id}/contacts/{contactId}", s.deactivateCustomerContact)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers/{id}/owners", s.listCustomerOwners)
		r.With(s.perm("masterdata:customer:write")).Post("/api/customers/{id}/owners", s.createCustomerOwner)
		r.With(s.perm("masterdata:customer:write")).Put("/api/customers/{id}/owners/{ownerId}", s.updateCustomerOwner)
		r.With(s.perm("masterdata:customer:write")).Delete("/api/customers/{id}/owners/{ownerId}", s.deactivateCustomerOwner)
		r.With(s.perm("masterdata:customer:read")).Get("/api/customers/{id}/changes", s.listCustomerChanges)
		r.With(s.perm("masterdata:supplier:read")).Get("/api/suppliers", s.listSuppliers)
		r.With(s.perm("masterdata:supplier:write")).Post("/api/suppliers", s.createSupplier)
		r.With(s.perm("masterdata:supplier:write")).Post("/api/suppliers/import", s.importSuppliers)
		r.With(s.perm("masterdata:supplier:read")).Get("/api/suppliers/countries", s.listSupplierCountries)
		r.With(s.perm("masterdata:supplier:read")).Get("/api/suppliers/duplicates", s.checkSupplierDuplicates)
		r.With(s.perm("masterdata:supplier:read")).Get("/api/suppliers/{id}", s.getSupplier)
		r.With(s.perm("masterdata:supplier:write")).Put("/api/suppliers/{id}", s.updateSupplier)
		r.With(s.perm("masterdata:supplier:write")).Delete("/api/suppliers/{id}", s.deactivateSupplier)
		r.With(s.perm("masterdata:supplier:read")).Get("/api/suppliers/{id}/deactivation-impact", s.getSupplierDeactivationImpact)
		r.With(s.perm("masterdata:supplier:write")).Post("/api/suppliers/{id}/activate", s.activateSupplier)
		r.With(s.perm("masterdata:supplier:read")).Get("/api/suppliers/{id}/contacts", s.listSupplierContacts)
		r.With(s.perm("masterdata:supplier:write")).Post("/api/suppliers/{id}/contacts", s.createSupplierContact)
		r.With(s.perm("masterdata:supplier:write")).Put("/api/suppliers/{id}/contacts/{contactId}", s.updateSupplierContact)
		r.With(s.perm("masterdata:supplier:write")).Delete("/api/suppliers/{id}/contacts/{contactId}", s.deactivateSupplierContact)
		r.With(s.perm("masterdata:supplier:read")).Get("/api/suppliers/{id}/owners", s.listSupplierOwners)
		r.With(s.perm("masterdata:supplier:write")).Post("/api/suppliers/{id}/owners", s.createSupplierOwner)
		r.With(s.perm("masterdata:supplier:write")).Put("/api/suppliers/{id}/owners/{ownerId}", s.updateSupplierOwner)
		r.With(s.perm("masterdata:supplier:write")).Delete("/api/suppliers/{id}/owners/{ownerId}", s.deactivateSupplierOwner)
		r.With(s.perm("masterdata:supplier:read")).Get("/api/suppliers/{id}/changes", s.listSupplierChanges)
		r.With(s.perm("masterdata:factory:read")).Get("/api/factories", s.listFactories)
		r.With(s.perm("masterdata:factory:write")).Post("/api/factories/import", s.importFactories)
		r.With(s.perm("masterdata:factory:read")).Get("/api/factories/countries", s.listFactoryCountries)
		r.With(s.perm("masterdata:factory:read")).Get("/api/factories/duplicates", s.checkFactoryDuplicates)
		r.With(s.perm("masterdata:factory:read")).Get("/api/factories/{id}", s.getFactory)
		r.With(s.perm("masterdata:factory:read")).Get("/api/factories/{id}/deactivation-impact", s.getFactoryDeactivationImpact)
		r.With(s.perm("masterdata:factory:write")).Post("/api/factories", s.createFactory)
		r.With(s.perm("masterdata:factory:write")).Put("/api/factories/{id}", s.updateFactory)
		r.With(s.perm("masterdata:factory:read")).Get("/api/factories/{id}/contacts", s.listFactoryContacts)
		r.With(s.perm("masterdata:factory:write")).Post("/api/factories/{id}/contacts", s.createFactoryContact)
		r.With(s.perm("masterdata:factory:write")).Put("/api/factories/{id}/contacts/{contactId}", s.updateFactoryContact)
		r.With(s.perm("masterdata:factory:write")).Delete("/api/factories/{id}/contacts/{contactId}", s.deactivateFactoryContact)
		r.With(s.perm("masterdata:factory:read")).Get("/api/factories/{id}/owners", s.listFactoryOwners)
		r.With(s.perm("masterdata:factory:write")).Post("/api/factories/{id}/owners", s.createFactoryOwner)
		r.With(s.perm("masterdata:factory:write")).Put("/api/factories/{id}/owners/{ownerId}", s.updateFactoryOwner)
		r.With(s.perm("masterdata:factory:write")).Delete("/api/factories/{id}/owners/{ownerId}", s.deactivateFactoryOwner)
		r.With(s.perm("masterdata:factory:read")).Get("/api/factories/{id}/capabilities", s.listFactoryCapabilities)
		r.With(s.perm("masterdata:factory:write")).Post("/api/factories/{id}/capabilities", s.createFactoryCapability)
		r.With(s.perm("masterdata:factory:write")).Delete("/api/factories/{id}/capabilities/{capabilityId}", s.deleteFactoryCapability)
		r.With(s.perm("masterdata:factory:read")).Get("/api/factories/{id}/certificates", s.listFactoryCertificates)
		r.With(s.perm("masterdata:factory:write")).Post("/api/factories/{id}/certificates", s.createFactoryCertificate)
		r.With(s.perm("masterdata:factory:write")).Delete("/api/factories/{id}/certificates/{certificateId}", s.deleteFactoryCertificate)
		r.With(s.perm("masterdata:factory:write")).Post("/api/factories/{id}/certificates/presign", s.presignFactoryCertificateFile)
		r.With(s.perm("masterdata:factory:read")).Get("/api/factories/{id}/changes", s.listFactoryChanges)
		r.With(s.perm("masterdata:port:read")).Get("/api/ports", s.listPorts)
		r.With(s.perm("masterdata:port:read")).Get("/api/ports/countries", s.listPortCountries)
		r.With(s.perm("masterdata:port:write")).Post("/api/ports/import", s.importPorts)
		r.With(s.perm("masterdata:port:write")).Post("/api/ports", s.createPort)
		r.With(s.perm("masterdata:port:read")).Get("/api/ports/{id}", s.getPort)
		r.With(s.perm("masterdata:port:write")).Put("/api/ports/{id}", s.updatePort)
		r.With(s.perm("masterdata:port:write")).Put("/api/ports/{id}/status", s.setPortStatus)
		r.With(s.perm("masterdata:port:read")).Get("/api/ports/{id}/deactivation-impact", s.getPortDeactivationImpact)
		r.With(s.perm("masterdata:port:read")).Get("/api/ports/{id}/changes", s.listPortChanges)
		// Option dictionaries feed every form's dropdowns; login is enough.
		r.Get("/api/options", s.listOptions)
		r.Post("/api/numbering/next", s.nextNumber)
		// Organisation and access control. Reading the directory is what every
		// picker needs; changing it is administrator work.
		r.With(s.perm("iam:department:read")).Get("/api/departments", s.listDepartments)
		r.With(s.perm("iam:department:write")).Post("/api/departments", s.createDepartment)
		r.With(s.perm("iam:department:write")).Put("/api/departments/{id}", s.updateDepartment)
		r.With(s.perm("iam:department:read")).Get("/api/departments/{id}/changes", s.listDepartmentChanges)
		r.With(s.perm("iam:employee:read")).Get("/api/employees", s.listEmployees)
		r.With(s.perm("iam:employee:read")).Get("/api/employees/{id}", s.getEmployee)
		r.With(s.perm("iam:employee:write")).Post("/api/employees", s.createEmployee)
		r.With(s.perm("iam:employee:write")).Put("/api/employees/{id}", s.updateEmployee)
		r.With(s.perm("iam:employee:read")).Get("/api/employees/{id}/changes", s.listEmployeeChanges)
		// 一批人的头像地址。走 read 权限，和员工列表同一道门——能看到这些人
		// 的人才能看到他们的照片，头像不单独设一套可见性。
		r.With(s.perm("iam:employee:read")).Post("/api/employees/avatar-urls", s.employeeAvatarURLs)
		// 组织架构图。一次返回两棵树要用的全部数据，走和员工列表同一道门。
		r.With(s.perm("iam:employee:read")).Get("/api/org-chart", s.orgChart)
		// 管理员替别人换/清头像。写权限，和改员工资料同一道门。
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/avatar/presign", s.presignEmployeeAvatar)
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/avatar", s.setEmployeeAvatar)
		r.With(s.perm("iam:employee:write")).Delete("/api/employees/{id}", s.deactivateEmployee)
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/activate", s.activateEmployee)
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/account", s.openAccount)
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/password", s.resetPassword)
		// Ends the sessions somebody is already holding. Its own action
		// because a stolen laptop is not a resignation, and deactivating is
		// not always what is wanted.
		r.With(s.perm("iam:employee:write")).
			Post("/api/employees/{id}/revoke-sessions", s.revokeSessions)
		// Sending the invitation is employee administration, so it carries the
		// same permission as creating the row it invites.
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/invite", s.inviteEmployee)
		// The administrator's 忘记密码-on-your-behalf. Same permission as the
		// direct reset it replaces for everyone who has a verified mailbox.
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/reset-link", s.sendResetLinkToEmployee)
		// The same act for a selection, with one report at the end. A separate
		// route rather than a list-shaped body on the one above, because the
		// answer has a different shape: per-person outcomes, not a status code.
		r.With(s.perm("iam:employee:write")).Post("/api/employees/invite-batch", s.inviteBatch)
		// Bringing a whole company in. dry_run in the body makes it a preview;
		// same route because it is the same validation either way.
		r.With(s.perm("iam:employee:write")).Post("/api/employees/import", s.importEmployees)
		r.With(s.perm("iam:role:write")).Post("/api/employees/{id}/roles", s.assignRoles)
		// The reporting line is org-chart maintenance, not access control,
		// so it sits with the rest of employee editing.
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/manager", s.setManager)
		r.With(s.perm("iam:role:read")).Get("/api/roles", s.listRoles)
		r.With(s.perm("iam:role:read")).Get("/api/roles/all", s.listAllRoles)
		r.With(s.perm("iam:role:write")).Post("/api/roles/{id}/status", s.setRoleStatus)
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
		// 「我的资料」。同样只要求登录：这几条的作用对象是调用者自己，
		// iam 那边根本不接受「员工 id」这个入参，所以它们**改不到别人**。
		// 挂 iam:employee:read 反而是错的——普通员工没有那个权限，
		// 而看自己的电话号码不该需要「查看员工」。
		r.Get("/api/me/profile", s.myProfile)
		r.Put("/api/me/profile", s.updateMyProfile)
		r.Post("/api/me/avatar/presign", s.presignMyAvatar)
		r.Post("/api/me/avatar", s.setMyAvatar)
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
		r.With(s.perm("export:quotation:read")).Get("/api/quotations/{id}/workbook", s.getQuotationWorkbook)
		r.With(s.perm("export:quotation:read")).Get("/api/quotations/{id}/pdf", s.getQuotationPDF)
		r.With(s.perm("export:quotation:write")).Post("/api/quotations", s.createQuotation)
		r.With(s.perm("export:quotation:write")).Put("/api/quotations/{id}", s.updateQuotation)
		r.With(s.perm("export:quotation:write")).Post("/api/quotations/{id}/send", s.sendQuotation)
		r.With(s.perm("export:quotation:write")).Post("/api/quotations/{id}/respond", s.respondQuotation)
		r.With(s.perm("export:quotation:write")).Post("/api/quotations/{id}/cancel", s.cancelQuotation)
		// Contracts. Signing is what makes a version binding, so it sits
		// behind the write permission like every other state change.
		r.With(s.perm("export:contract:read")).Get("/api/contracts", s.listContracts)
		// 合同执行一览（D2）。挂在合同的读权限下，因为这一行问的就是
		// 「我这张合同办到哪了」——看得见合同才看得见它的进度。
		r.With(s.perm("export:contract:read")).Get("/api/contract-execution", s.listContractExecution)
		r.With(s.perm("export:contract:read")).Get("/api/contracts/{id}", s.getContract)
		r.With(s.perm("export:contract:write")).Post("/api/contracts", s.createContract)
		r.With(s.perm("export:contract:write")).Post("/api/contracts/direct", s.createDirectContract)
		r.With(s.perm("export:contract:write")).Post("/api/contracts/existing", s.importExistingContract)
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
		// Shipping documents. A separate permission from contracts: the
		// forwarder desk types bills of lading and must not be able to alter a
		// price, while sales must see where the goods are without being able
		// to declare a boat sailed.
		r.With(s.perm("export:shipment:read")).Get("/api/shipments", s.listShipments)
		r.With(s.perm("export:shipment:read")).Get("/api/shipments/{id}", s.getShipment)
		r.With(s.perm("export:shipment:write")).Post("/api/shipments", s.createShipment)
		r.With(s.perm("export:shipment:write")).Put("/api/shipments/{id}", s.updateShipment)
		// Confirming the sailing is what moves goods in every contract's
		// progress view, so it is a write even though it changes no numbers
		// the user typed.
		r.With(s.perm("export:shipment:write")).Post("/api/shipments/{id}/confirm", s.confirmSailing)
		r.With(s.perm("export:shipment:write")).Post("/api/shipments/{id}/arrive", s.markShipmentArrived)
		r.With(s.perm("export:shipment:write")).Post("/api/shipments/{id}/cancel", s.cancelShipment)
		// Which boats one contract's goods are on is part of the contract detail.
		// Sales owners need it to follow the contract they can already read, but
		// must not gain access to the shipment module's lists or write actions.
		r.With(s.perm("export:contract:read")).Get("/api/contracts/{id}/vessels", s.listContractVessels)
		r.With(s.perm("shipping:schedule:read")).Get("/api/shipping/status", s.getShippingStatus)
		r.With(s.perm("shipping:schedule:read")).Get("/api/shipping/responsible-options", s.listVisibleEmployees)
		r.With(s.perm("shipping:schedule:read")).Get("/api/shipping/schedules", s.listShippingSchedules)
		r.With(s.perm("shipping:schedule:read")).Get("/api/shipping/contract-handoffs", s.listContractShippingHandoffs)
		// 售前任务列表属于船运操作台，而不是采购/销售的只读案件视图。
		// 后两者通过各自的 sourcing case collaboration 接口查看结果，
		// 不能凭普通船期只读权限进入船运人员的工作队列。
		r.With(s.perm("shipping:sourcing:read")).Get("/api/shipping/sourcing-tasks", s.listSourcingShippingTasks)
		r.With(s.perm("shipping:sourcing:read")).Get("/api/shipping/sourcing-tasks/{id}", s.getShippingSourcingTask)
		r.With(s.perm("shipping:sourcing:write")).Post("/api/shipping/sourcing-tasks/{id}/start", s.startSourcingShippingTask)
		r.With(s.perm("shipping:sourcing:write")).Post("/api/shipping/sourcing-tasks/{id}/join", s.joinSourcingShippingTask)
		r.With(s.perm("shipping:sourcing:write")).Post("/api/shipping/sourcing-tasks/{id}/primary-request", s.requestPrimaryShipping)
		r.With(s.perm("shipping:sourcing:write")).Post("/api/shipping/sourcing-tasks/{id}/options", s.addSourcingShippingOption)
		r.With(s.perm("shipping:sourcing:approve")).Post("/api/shipping/sourcing-tasks/{id}/primary", s.assignPrimaryShipping)
		r.With(s.perm("shipping:sourcing:approve")).Post("/api/shipping/sourcing-tasks/{id}/plans", s.createSourcingShippingPlan)
		r.With(s.perm("shipping:sourcing:approve")).Post("/api/shipping/sourcing-plans/{id}/submit-to-sales", s.submitSourcingShippingPlan)
		r.With(s.perm("shipping:schedule:read")).Get("/api/shipping/statistics", s.getShippingStatistics)
		r.With(s.perm("shipping:schedule:read")).Get("/api/shipping/reminders", s.listShippingArrivalNotifications)
		// 提单签发提醒按登录人隔离，同时要求具备船期读取权限，避免首页或徽标泄露受限业务摘要。
		r.With(s.perm("shipping:schedule:read")).Get("/api/bl-reminders", s.listBLReminders)
		r.With(s.perm("shipping:schedule:read")).Post("/api/bl-reminders/read", s.markBLRemindersRead)
		r.With(s.perm("shipping:schedule:read")).Delete("/api/shipping/reminders/expired", s.cleanupExpiredShippingArrivalReminders)
		r.With(s.perm("shipping:schedule:read")).Post("/api/shipping/reminders/{reminderID}/read", s.markShippingArrivalReminderRead)
		r.With(s.perm("shipping:schedule:read")).Get("/api/shipping/schedules/{id}", s.getShippingSchedule)
		r.With(s.perm("shipping:schedule:write")).Post("/api/shipping/schedules", s.createShippingSchedule)
		r.With(s.perm("shipping:schedule:write")).Put("/api/shipping/schedules/{id}", s.updateShippingSchedule)
		r.With(s.perm("shipping:schedule:read")).Get("/api/shipping/schedules/{id}/reminder-rules", s.getShippingArrivalReminderRules)
		r.With(s.perm("shipping:schedule:write")).Put("/api/shipping/schedules/{id}/reminder-rules", s.updateShippingArrivalReminderRules)
		r.With(s.perm("shipping:schedule:write")).Post("/api/shipping/schedules/{id}/status", s.updateShippingScheduleStatus)
		r.With(s.perm("shipping:schedule:write")).Post("/api/shipping/schedules/{id}/cancel", s.cancelShippingSchedule)
		r.With(s.perm("shipping:route:write")).Post("/api/shipping/schedules/{id}/route/nodes", s.addShippingRouteNode)
		r.With(s.perm("shipping:route:write")).Delete("/api/shipping/schedules/{id}/route/nodes/{nodeID}", s.removeShippingRouteNode)
		r.With(s.perm("shipping:route:write")).Put("/api/shipping/schedules/{id}/route/order", s.reorderShippingRoute)
		r.With(s.perm("shipping:progress:write")).Post("/api/shipping/schedules/{id}/progress", s.updateShippingProgress)
		r.With(s.perm("shipping:document:view")).Get("/api/shipping/schedules/{id}/documents", s.listShippingDocuments)
		r.With(s.perm("shipping:document:upload")).Post("/api/shipping/schedules/{id}/documents/presign", s.presignShippingDocument)
		r.With(s.perm("shipping:document:upload")).Post("/api/shipping/schedules/{id}/documents", s.registerShippingDocument)
		r.With(s.perm("shipping:document:view")).Post("/api/shipping/schedules/{id}/documents/{documentID}/preview", s.previewShippingDocument)
		r.With(s.perm("shipping:document:download")).Post("/api/shipping/schedules/{id}/documents/{documentID}/download", s.downloadShippingDocument)
		r.With(s.perm("shipping:document:invalidate")).Post("/api/shipping/schedules/{id}/documents/{documentID}/invalidate", s.invalidateShippingDocument)
		// Collection. Its own permission because it is finance work: the
		// person who reconciles bank lines is not the person who sells, and
		// neither should be able to do the other's job by accident.
		r.With(s.perm("export:receipt:read")).Get("/api/bank-accounts", s.listBankAccounts)
		r.With(s.perm("export:receipt:write")).Post("/api/bank-accounts", s.createBankAccount)
		// 「收款对账」那一组地址整体下线了（/api/receipt-transactions/*、
		// /api/receipt-allocations/{id}/reverse、/api/open-receivables）。
		//
		// 需求变更之后核销不再从一行银行流水出发：财务在「待核销」页上选一张
		// 合同、手填金额，走下面的 /api/receivable-due/{id}/receipts。旧的那
		// 一组是**另一条写路径**——它写的核销行挂着 bank_txn_id，绕过新页面
		// 的形状——留着一个没有界面、却仍然能写账的入口，是下一次「数据怎么
		// 会变成这样」的起点，所以一并摘掉。
		//
		// 服务端的实现和 proto 上的 RPC 都留在原地：老数据里挂着流水的核销行
		// 还要能冲销，那条路由 ReverseContractReceipt 内部转交（见
		// export/internal/app/receivable.go），它带着行锁、认差结清守门和
		// 认领量回写，是新入口缺的那三样。
		// 应收到期清单（E1）：沿用收款的读权限——能看收款的人就该看得见该收什么。
		r.With(s.perm("export:receipt:read")).Get("/api/receivable-due", s.listReceivableDue)
		// 收款结清：停催是对钱的判断，走收款的写权限。
		// 待核销页上的两个动作：记一笔收款、冲销记错的那笔。
		// {id} 一个是合同、一个是收款记录，所以地址前缀故意不同。
		r.With(s.perm("export:receipt:write")).Post("/api/receivable-due/{id}/receipts", s.recordContractReceipt)
		r.With(s.perm("export:receipt:write")).Post("/api/receivable-due/manual", s.createManualReceivable)
		r.With(s.perm("export:receipt:write")).Post("/api/contract-receipts/{id}/reverse", s.reverseContractReceipt)
		r.With(s.perm("export:receipt:write")).Post("/api/receivable-due/{id}/close", s.closeReceivable)
		r.With(s.perm("export:receipt:write")).Post("/api/receivable-due/{id}/reopen", s.reopenReceivable)
		// 事后改一份合同的应收到期日。合同的常规编辑口只对草稿开放、且只放
		// 销售属主过，所以财务这条路自己开一个门，和「确认完成」并排。
		r.With(s.perm("export:receipt:write")).Post("/api/receivable-due/{id}/due-date", s.setReceivableDueDate)
		// 应收提醒按登录人隔离，同时要求具备收款读取权限，避免首页或徽标成为权限后门。
		r.With(s.perm("export:receipt:read")).Get("/api/receivable-reminders", s.listReceivableReminders)
		r.With(s.perm("export:receipt:read")).Post("/api/receivable-reminders/read", s.markReceivableRemindersRead)
		// Attribute templates. Defining what a category's spec looks like is
		// catalogue maintenance, so it rides on the product write permission
		// rather than inventing a third one for the same job.
		r.With(s.perm("product:product:read")).Get("/api/categories/{id}/attributes", s.resolveAttributes)
		r.With(s.perm("product:product:read")).Get("/api/categories/{id}/attribute-template", s.getAttributeTemplate)
		r.With(s.perm("product:product:write")).Put("/api/categories/{id}/attribute-template", s.saveAttributeTemplate)
		r.With(s.perm("product:product:write")).Put("/api/products/{id}/attributes", s.setProductAttributes)
		r.With(s.perm("product:product:write")).Put("/api/skus/{id}/attributes", s.setSkuAttributes)
		// Candidate recall. A read of the catalogue, so it is gated as one —
		// it will later be called by the request-sheet parser as well as by
		// somebody typing in the product picker.
		r.With(s.perm("product:product:read")).Get("/api/product-recall", s.recallCandidates)
		// How much of one contract has been collected. Gated on reading
		// contracts rather than receipts: this is the salesperson's view of
		// their own deal, not the finance queue.
		r.With(s.perm("export:contract:read")).Get("/api/contracts/{id}/receipts", s.getContractReceipts)
		// Handing a deal to somebody else is a supervisor's act, so it gets its
		// own permission rather than riding on :write — the people who may edit
		// their own documents are exactly the people who may not reassign them.
		// Purchase requirements. They are never created through the API: they
		// arrive from contracts that took effect, over Kafka. A buyer reads
		// them and closes the ones the business will not act on.
		// Stock. Reading it is what every sourcing decision starts from, so it
		// sits with the rest of the operational reads.
		r.With(s.perm("inventory:stock:read")).Get("/api/warehouses", s.listWarehouses)
		r.With(s.perm("inventory:stock:write")).Post("/api/warehouses", s.createWarehouse)
		r.With(s.perm("inventory:stock:write")).Put("/api/warehouses/{id}", s.updateWarehouse)
		r.With(s.perm("inventory:stock:read")).Get("/api/warehouse-settings", s.getWarehouseSettings)
		r.With(s.perm("inventory:stock:write")).Put("/api/warehouse-settings", s.updateWarehouseSettings)
		r.With(s.perm("inventory:stock:read")).Get("/api/stocks", s.listStocks)
		r.With(s.perm("inventory:stock:read")).Get("/api/stocks/{id}", s.getStock)
		r.With(s.perm("inventory:stock:read")).Get("/api/stock-ledger", s.listStockLedger)
		r.With(s.perm("inventory:stock:write")).Post("/api/stocks/receive", s.receiveStock)
		r.With(s.perm("inventory:stock:write")).Post("/api/stocks/{id}/freeze", s.freezeStock)
		r.With(s.perm("inventory:stock:write")).Post("/api/stocks/{id}/unfreeze", s.unfreezeStock)
		r.With(s.perm("inventory:stock:import")).Get("/api/stock-imports/template", s.downloadStockImportTemplate)
		r.With(s.perm("inventory:stock:import")).Post("/api/stock-imports/preview", s.previewInitialStockImport)
		r.With(s.perm("inventory:stock:import")).Get("/api/stock-imports", s.listStockImports)
		r.With(s.perm("inventory:stock:import")).Post("/api/stock-imports/{importToken}/confirm", s.confirmInitialStockImport)
		r.With(s.perm("inventory:stock:import")).Get("/api/stock-imports/{importToken}/report", s.downloadStockImportReport)
		r.With(s.perm("inventory:stock:import")).Post("/api/stock-imports/{importToken}/cancel", s.cancelStockImport)
		// Outbound. Reading what is shippable is a stock read; taking goods
		// off the shelf is a stock write, and the same permission covers both
		// directions of movement.
		r.With(s.perm("inventory:stock:read")).Get("/api/shippable", s.listShippable)
		r.With(s.perm("inventory:stock:read")).Get("/api/shippable/{id}/lines", s.listShippableLines)
		r.With(s.perm("inventory:stock:read")).Get("/api/outbounds", s.listOutbounds)
		r.With(s.perm("inventory:stock:read")).Get("/api/outbounds/{id}/items", s.listOutboundItems)
		r.With(s.perm("inventory:stock:write")).Post("/api/outbounds", s.createOutbound)
		r.With(s.perm("inventory:stock:write")).Post("/api/outbounds/{id}/confirm", s.confirmOutbound)
		r.With(s.perm("inventory:stock:write")).Post("/api/outbounds/{id}/cancel", s.cancelOutbound)
		r.With(s.perm("procurement:requirement:read")).Get("/api/requirements", s.listRequirements)
		r.With(s.perm("procurement:requirement:read"), s.perm("procurement:order:write")).Post("/api/requirements/purchase-template/export", s.exportPurchaseTemplate)
		// 客户询盘由销售维护；客户选择项不因此开放完整客户档案菜单。
		r.With(s.perm("sales:inquiry:write")).Get("/api/sourcing-customer-options", s.listSourcingCustomerOptions)
		r.With(s.perm("sales:inquiry:write")).Get("/api/sourcing-customer-options/{id}/contacts", s.listSourcingCustomerContacts)
		r.With(s.perm("sales:inquiry:read")).Get("/api/sales-inquiries", s.listSourcingCases)
		r.With(s.perm("sales:inquiry:read")).Get("/api/sales-inquiries/{id}", s.getSourcingCase)
		r.With(s.perm("sales:inquiry:read")).Get("/api/sales-inquiries/{id}/changes", s.listSourcingCaseChanges)
		r.With(s.perm("sales:inquiry:write")).Post("/api/sales-inquiries/{id}/withdraw", s.withdrawSourcingCase)
		r.With(s.perm("sales:procurement-progress:read")).Get("/api/sales-inquiries/{id}/procurement-progress", s.getSalesProcurementProgress)
		r.With(s.perm("sales:inquiry:read")).Get("/api/sales-inquiries/{id}/shipping-collaboration", s.getCaseShippingCollaboration)
		r.With(s.perm("sales:inquiry:read")).Get("/api/sales-inquiries/{id}/sales-plans", s.listSalesPlans)
		r.With(s.perm("sales:inquiry:write")).Post("/api/sales-inquiries/{id}/sales-plans", s.createSalesPlan)
		r.With(s.perm("sales:inquiry:read")).Get("/api/sales-inquiries/{id}/customer-feedback", s.listCustomerFeedback)
		r.With(s.perm("sales:inquiry:write")).Post("/api/sales-inquiries/{id}/customer-feedback", s.addCustomerFeedback)
		r.With(s.perm("sales:inquiry:write")).Post("/api/sales-inquiries/{id}/procurement-reworks", s.createSalesProcurementRework)
		r.With(s.perm("sales:inquiry:read")).Get("/api/sales-inquiries/{id}/shipping-reworks", s.listShippingReworks)
		r.With(s.perm("sales:inquiry:write")).Post("/api/sales-inquiries/{id}/shipping-reworks", s.createShippingRework)
		r.With(s.perm("sales:inquiry:read")).Get("/api/sales-inquiries/{id}/customer-selections", s.listCustomerSelections)
		r.With(s.perm("sales:inquiry:write")).Post("/api/sales-inquiries/{id}/customer-selections", s.confirmCustomerSelection)
		r.With(s.perm("sales:inquiry:write")).Post("/api/sales-inquiries/{id}/customer-selections/decision", s.decideCustomerSelection)
		r.With(s.perm("export:quotation:write")).Post("/api/sales-inquiries/{id}/customer-selections/{selectionId}/formal-quotation", s.createFormalQuotationFromSelection)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/sourcing-cases", s.listSourcingCases)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/sourcing-cases/{id}", s.getSourcingCase)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/sourcing-cases/{id}/changes", s.listSourcingCaseChanges)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/sourcing-cases/{id}/participants", s.listSourcingParticipants)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/sourcing-cases/{id}/shipping-collaboration", s.getCaseShippingCollaboration)
		r.With(s.perm("sales:inquiry:write")).Post("/api/sourcing-cases", s.createSourcingCase)
		r.With(s.perm("sales:inquiry:write")).Post("/api/sourcing-intakes/import", s.importSourcingIntake)
		r.With(s.perm("sales:inquiry:write")).Post("/api/sourcing-cases/{id}/lines", s.addSourcingLine)
		// 标准询盘模板属于销售接收客户需求的工具。
		r.With(s.perm("sales:inquiry:read")).Get("/api/inquiry-templates", s.listInquiryTemplates)
		r.With(s.perm("sales:inquiry:read")).Get("/api/inquiry-templates/{id}", s.getInquiryTemplate)
		r.With(s.perm("sales:inquiry:read")).Get("/api/inquiry-templates/{id}/download", s.downloadInquiryTemplate)
		r.With(s.perm("sales:inquiry:write")).Post("/api/inquiry-templates", s.createInquiryTemplate)
		r.With(s.perm("sales:inquiry:write")).Put("/api/inquiry-templates/{id}", s.saveInquiryTemplate)
		r.With(s.perm("sales:inquiry:write")).Post("/api/inquiry-templates/{id}/status", s.setInquiryTemplateStatus)
		r.With(s.perm("sales:inquiry:write")).Post("/api/inquiry-templates/{id}/default", s.setDefaultInquiryTemplate)
		r.With(s.perm("sales:inquiry:submit")).Post("/api/sourcing-cases/{id}/confirm-lines", s.confirmSourcingLines)
		r.With(s.perm("procurement:sourcing:write")).Post("/api/sourcing-cases/{id}/accept", s.acceptSourcingCase)
		r.With(s.perm("procurement:sourcing:write")).Post("/api/sourcing-cases/{id}/participants", s.joinSourcingCase)
		r.With(s.perm("procurement:sourcing:write")).Post("/api/sourcing-cases/{id}/primary-request", s.requestPrimarySourcingCase)
		r.With(s.perm("procurement:sourcing:approve")).Post("/api/sourcing-cases/{id}/primary-buyer", s.assignPrimarySourcingCase)
		r.With(s.perm("procurement:sourcing:write")).Post("/api/sourcing-cases/{id}/return", s.returnSourcingCase)
		r.With(s.perm("sales:inquiry:write"), s.perm("product:product:read")).Put("/api/sourcing-cases/{id}/lines/{lineId}", s.reviewSourcingLine)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/sourcing-cases/{id}/factory-rfqs", s.listFactoryRFQs)
		r.With(s.perm("procurement:sourcing:write")).Post("/api/sourcing-cases/{id}/factory-rfqs", s.createFactoryRFQ)
		r.With(s.perm("procurement:sourcing:write")).Put("/api/factory-rfqs/{id}", s.updateFactoryRFQ)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/factory-rfqs/overdue", s.listOverdueFactoryRFQs)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/sourcing-cases/{id}/supplier-quotes", s.listSupplierQuoteComparison)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/sourcing-cases/{id}/procurement-plans", s.listProcurementPlans)
		r.With(s.perm("procurement:sourcing:approve")).Post("/api/sourcing-cases/{id}/procurement-plans", s.createProcurementPlan)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/procurement-plans/{id}", s.getProcurementPlan)
		r.With(s.perm("procurement:sourcing:approve")).Post("/api/procurement-plans/{id}/submit-to-sales", s.submitProcurementPlanToSales)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/sourcing-cases/{id}/procurement-reworks", s.listProcurementReworks)
		r.With(s.perm("procurement:sourcing:approve")).Post("/api/sourcing-cases/{id}/procurement-reworks", s.createProcurementRework)
		r.With(s.perm("procurement:sourcing:price")).Post("/api/procurement-reworks/{id}/resolve", s.resolveProcurementRework)
		r.With(s.perm("shipping:sourcing:read")).Get("/api/shipping/sourcing-reworks", s.listMyShippingReworks)
		r.With(s.perm("shipping:sourcing:write")).Post("/api/shipping/sourcing-reworks/{id}/resolve", s.resolveShippingRework)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/sourcing-cases/{id}/cost-scenarios", s.listCostScenarios)
		r.With(s.perm("procurement:sourcing:price")).Post("/api/sourcing-cases/{id}/cost-scenarios", s.createCostScenario)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/cost-scenarios/{id}", s.getCostScenario)
		r.With(s.perm("procurement:sourcing:approve")).Post("/api/cost-scenarios/{id}/confirm", s.confirmCostScenario)
		r.With(s.perm("procurement:sourcing:approve")).Post("/api/cost-scenarios/{id}/submit-to-sales", s.submitCostToSales)
		// 采购经理确认成本，到此采购责任结束；生成客户报价只由销售报价权限控制。
		r.With(s.perm("export:quotation:write")).Post("/api/cost-scenarios/{id}/create-customer-quotation", s.createCustomerQuotationFromCost)
		r.With(s.perm("procurement:sourcing:price")).Post("/api/factory-rfqs/{id}/supplier-quotes", s.createSupplierQuote)
		r.With(s.perm("procurement:sourcing:read")).Get("/api/factory-rfqs/{id}/workbook", s.getFactoryRFQWorkbook)
		r.With(s.perm("procurement:sourcing:price")).Post("/api/factory-rfqs/{id}/supplier-quotes/import", s.importSupplierQuoteWorkbook)
		r.With(s.perm("procurement:sourcing:send")).Post("/api/factory-rfqs/{id}/send", s.sendFactoryRFQ)
		r.With(s.perm("procurement:requirement:read")).Get("/api/requirements/{id}", s.getRequirement)
		r.With(s.perm("procurement:requirement:exception")).Post("/api/requirements", s.createRequirement)
		r.With(s.perm("procurement:requirement:write")).Post("/api/requirements/{id}/cancel", s.cancelRequirement)
		r.With(s.perm("procurement:requirement:write")).Post("/api/requirements/{id}/reopen", s.reopenRequirement)
		// Which orders cover a requirement. Reading orders, so it rides on the
		// order permission: somebody who may not see purchasing commitments
		// should not see them through this door either.
		r.With(s.perm("procurement:order:read")).Get("/api/requirements/{id}/orders", s.listRequirementOrders)
		// Purchase orders. A separate permission from requirements on purpose:
		// reading what has to be bought and committing company money to a
		// supplier are different jobs, and often different people.
		r.With(s.perm("procurement:order:read")).Get("/api/purchase-orders", s.listOrders)
		r.With(s.perm("procurement:order:read")).Get("/api/purchase-orders/{id}", s.getOrder)
		r.With(s.perm("procurement:order:write")).Post("/api/purchase-orders/imports/preview", s.previewOrderImport)
		r.With(s.perm("procurement:order:write")).Post("/api/purchase-orders/template-imports/preview", s.previewPurchaseTemplateImport)
		r.With(s.perm("procurement:order:write")).Post("/api/purchase-orders/imports/{importToken}/confirm", s.confirmOrderImport)
		// 待采购页面的确认会直接形成公司采购承诺，因此创建接口同时要求
		// 建单和审批权限，避免绕过唯一的人工审批入口。
		r.With(
			s.perm("procurement:order:write"),
			s.perm("procurement:order:submit"),
		).Post("/api/purchase-orders", s.createOrder)
		r.With(s.perm("procurement:order:write")).Put("/api/purchase-orders/{id}", s.updateOrder)
		r.With(s.perm("procurement:order:submit")).Post("/api/purchase-orders/{id}/submit", s.submitOrder)
		r.With(s.perm("procurement:order:cancel")).Post("/api/purchase-orders/{id}/cancel", s.cancelOrder)
		r.With(s.perm("procurement:order:read")).Get("/api/purchase-orders/{id}/documents", s.getOrderDocuments)
		r.With(s.perm("procurement:order:send")).Post("/api/purchase-orders/{id}/send", s.sendPurchaseOrder)
		r.With(s.perm("procurement:order:read")).Get("/api/purchase-orders/{id}/execution", s.getOrderExecution)
		r.With(s.perm("procurement:production:write")).Post("/api/purchase-orders/{id}/supplier-confirmations", s.recordSupplierConfirmation)
		r.With(s.perm("procurement:production:write")).Post("/api/purchase-orders/{id}/production-milestones", s.saveProductionMilestone)

		// 供应商这边只剩「供应商对账」一个界面：一张采购单一行，员工手填
		// 核销数字、手动确认完成。发票页、付款页、往来汇总页都已下线。
		r.With(s.perm("procurement:recon:read")).Get("/api/supplier-recon", s.listSupplierRecon)
		r.With(s.perm("procurement:recon:read")).Get("/api/supplier-recon/{id}/payments", s.listPurchaseOrderPayments)
		r.With(s.perm("procurement:recon:write")).Post("/api/supplier-recon/{id}/payments", s.recordPurchaseOrderPayment)
		r.With(s.perm("procurement:recon:write")).Post("/api/supplier-recon/manual", s.createManualPayable)
		r.With(s.perm("procurement:recon:write")).Post("/api/supplier-recon/{id}/payments/{allocationId}/reverse", s.reversePurchaseOrderPayment)
		r.With(s.perm("procurement:recon:write")).Post("/api/supplier-recon/{id}/close", s.closePurchaseOrderPayment)
		r.With(s.perm("procurement:recon:write")).Post("/api/supplier-recon/{id}/reopen", s.reopenPurchaseOrderPayment)
		// 事后改一张已下单采购单的应付到期日。采购单本身已下单之后没有编辑
		// 入口（表单只对草稿和被驳回的单开放），所以这个门开在对账页上。
		r.With(s.perm("procurement:recon:write")).Post("/api/supplier-recon/{id}/due-date", s.setPayableDueDate)
		// 挂在采购单上的凭证——发票扫描件、水单、退款回执。发票页下线之后，
		// 「留凭证」这件事搬到了这里；一张单可以有好几份。
		r.With(s.perm("procurement:recon:read")).Get("/api/supplier-recon/{id}/files", s.listReconFiles)
		r.With(s.perm("procurement:recon:write")).Post("/api/supplier-recon/{id}/files/presign", s.presignReconFile)
		r.With(s.perm("procurement:recon:write")).Post("/api/supplier-recon/{id}/files", s.attachReconFile)
		r.With(s.perm("procurement:recon:write")).Post("/api/supplier-recon/files/{fileId}/remove", s.removeReconFile)

		// ── 下面这三组地址整体下线了 ──────────────────────────
		//
		//   /api/supplier-invoices/*    发票录入、作废、三单匹配、扫描件
		//   /api/supplier-payments/*    建付款单、把它拆到发票/采购单、冲销
		//   /api/supplier-statements/*  按供应商 × 币种的往来汇总
		//
		// 需求收敛成「供应商这边只留一个和客户对账类似的供应商对账」。这三页
		// 一起下线，它们的写路径也跟着摘掉——没有界面却仍能写账的入口，是
		// 下一次「数据怎么会变成这样」的起点，客户侧退役收款对账时立的就是
		// 这条规矩。
		//
		// **服务端实现和 proto 上的 RPC 全部留在原地。** 三个理由：
		//   · 历史数据还要读得出来——对账页的「已付」里有一大块是走发票那条
		//     路进来的，它读的是 supplier_invoice_lines
		//   · 历史的 payment-backed 核销行还要冲得掉，对账页在服务层转交给
		//     ReverseSupplierPaymentAllocation（见 supplierrecon.go）
		//   · buf 的破坏性检查不允许删 RPC
		//
		// 权限码 procurement:invoice:* 从此没有使用者，但**不删**：删码要动
		// 角色授权，而留着一个没人用的码不会让任何东西出错。
		// procurement:payment:* 仍在用——银行流水那一组路由挂的就是它。
		// Bank rows are payment data: one spend chain, one knob.
		r.With(s.perm("procurement:payment:read")).Get("/api/bank-transactions", s.listBankTransactions)
		// 手工登记一行流水。CSV 之外的另一条入口，RPC 早就有（收款对账那边
		// 一直在用），只是网关从没给它开过 HTTP 路由——于是银行还没出对账单、
		// 财务想先把一笔出账记下来的时候，唯一的办法是伪造一行 CSV 导进去。
		r.With(s.perm("procurement:payment:write")).Post("/api/bank-transactions", s.recordBankTransaction)
		r.With(s.perm("procurement:payment:write")).Post("/api/bank-transactions/import", s.importBankStatement)
		// **只留 unmatch，不留 match。** 匹配这件事下线了（付款单没有创建
		// 入口，且和「流水只是记录」的新模型冲突），但**解开历史匹配**必须
		// 留着：SetBankTransactionOwnership 那道闸遇到已匹配的行会拒绝，
		// 并让人「先取消匹配」——把这条路一起删掉，那句话就成了一个做不到
		// 的指令，那些行的归属从此谁也改不了。
		//
		// 这和别处同一条纪律：新路不再走了，老数据的回退口子留着。
		r.With(s.perm("procurement:payment:write")).Post("/api/bank-transactions/{id}/unmatch", s.unmatchBankTransaction)
		// /api/bank-transactions/{id}/match 下线了。
		//
		// 它们是「把一行流水对上一张供应商付款单」，而付款单的唯一创建入口
		// 随供应商付款页一起没了——候选池只减不增，留着就是一个会慢慢归零
		// 的按钮。更要紧的是它和新模型本来就冲突：需求原话是「银行流水这些
		// 都只是用来记录」，不参与核销。流水页现在回归纯记录 + 凭证。
		//
		// MatchBankTransaction 的服务端实现留着：存量数据里已经匹配上的
		// 那些行还要读得出来（列表上照常显示付款单号）。
		// 那份对账单（PDF）。挂在登记流水同一个权限下——能记这笔钱的人，
		// 就该能把银行给的那张纸传上来。
		r.With(s.perm("procurement:payment:write")).Post("/api/bank-transactions/{id}/attachment/presign", s.presignBankTransactionFile)
		r.With(s.perm("procurement:payment:write")).Post("/api/bank-transactions/{id}/attachment", s.attachBankTransactionFile)
		// 归属：这笔钱是谁那条线上的。见 docs/开发计划.md F2。
		r.With(s.perm("procurement:payment:write")).Post("/api/bank-transactions/{id}/ownership", s.setBankTransactionOwnership)
		// 删一条流水——归档，不是抹掉。理由必填，所以是 POST 带 body 而不是
		// DELETE：带 body 的 DELETE 在代理和客户端那层各家实现不一，丢掉
		// body 的后果是「你明明填了理由，它说你没填」。
		r.With(s.perm("procurement:payment:write")).Post("/api/bank-transactions/{id}/delete", s.deleteBankTransaction)
		r.With(s.perm("procurement:payment:write")).Post("/api/bank-transactions/{id}/restore", s.restoreBankTransaction)
		// 改一行流水。理由必填，每处改动留痕（00047）。PUT 因为这是「把整行
		// 替换成新的样子」；删除那边用 POST 是因为 DELETE 带 body 不可靠，
		// PUT 没有这个问题。
		r.With(s.perm("procurement:payment:write")).Put("/api/bank-transactions/{id}", s.updateBankTransaction)
		// 这一行被改过什么。读，所以是 read 权限。
		r.With(s.perm("procurement:payment:read")).Get("/api/bank-transactions/{id}/changes", s.listBankTransactionChanges)
		r.With(s.perm("procurement:exception:write")).Post("/api/purchase-orders/{id}/exceptions", s.reportReceiptException)
		r.With(s.perm("procurement:exception:write")).Post("/api/purchase-orders/{id}/exceptions/{exceptionId}/resolve", s.resolveReceiptException)
		// 质检与到货异常同一职责同一旋钮；结案是生命周期决定，单独的码。
		r.With(s.perm("procurement:exception:write")).Post("/api/purchase-orders/{id}/inspections", s.recordInspection)
		r.With(s.perm("procurement:exception:write")).Post("/api/purchase-orders/{id}/inspections/{inspectionId}/resolve", s.resolveInspection)
		r.With(s.perm("procurement:order:close")).Post("/api/purchase-orders/{id}/close", s.closePurchaseOrder)
		// Receiving is warehouse work, so it rides on the stock permission
		// rather than the buyer's.
		r.With(s.perm("procurement:receipt:write")).Post("/api/purchase-orders/{id}/receive", s.receiveOrder)
		r.With(s.perm("export:ownership:transfer")).Post("/api/ownership/transfer", s.transferOwnership)
		// Reading the handover history is scoped like reading the document, so
		// the contract's own permission is the right gate.
		r.With(s.perm("export:contract:read")).Get("/api/ownership/transfers", s.listOwnershipTransfers)
		// 个人审批查询从认证元数据取得员工编号；读取本人事项与审批动作分开，
		// 真正执行通过、驳回或退回仍由下面的动作权限控制。
		r.Get("/api/approvals/todos", s.myTodos)
		r.Get("/api/approvals/submitted", s.mySubmittedApprovals)
		// HOME2 只聚合当前员工有权读取的现有提醒，不复制业务数据。
		r.Get("/api/home/reminders", s.listHomeReminders)
		r.Post("/api/home/reminders/read", s.markHomeRemindersRead)
		r.With(s.perm("approval:task:act")).Post("/api/approvals/tasks/{id}/act", s.actOnTask)
		// Reading where a document stands is not acting on it: the salesperson
		// who submitted a contract needs to see it is waiting on the sales
		// manager without any power to approve anything.
		r.With(s.perm("approval:instance:read")).Get("/api/approvals/instances", s.listApprovalInstances)
		r.With(s.perm("approval:instance:read")).Get("/api/approvals/instances/{id}", s.getApprovalInstance)
		// Correspondence. Reading is scoped by the notification data scope —
		// the permission only says "may open the mail module at all", the
		// scope decides whose mail comes back.
		// The address book the composer picks from. Gated on sending: it is
		// only ever used to choose who a mail goes to.
		r.With(s.perm("mail:email:write")).Get("/api/mailing-contacts", s.listMailingContacts)
		// The same book grouped by country, and one country's worth of it.
		// Same permission: this is the composer's picker, not the customer list.
		r.With(s.perm("mail:email:write")).Get("/api/customer-countries", s.listCustomerCountries)
		r.With(s.perm("mail:email:write")).Get("/api/mailing-contacts/by-country", s.contactsInCountry)
		// The supervisor's employee picker. Scoped by the same notification
		// data scope, so it lists exactly whose mail the caller may open.
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/email-senders", s.listMailSenders)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/email-campaigns", s.listCampaigns)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/email-campaigns/{id}", s.getCampaign)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/email-messages", s.listEmailMessages)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/email-messages/{id}", s.getEmailMessage)
		// Previewing renders against a real contact but sends nothing, so it
		// is gated with sending rather than reading: only somebody who could
		// send this mail has any business rendering it.
		r.With(s.perm("mail:email:write"), s.requireMailUnlock).Post("/api/email-campaigns/preview", s.previewCampaign)
		r.With(s.perm("mail:email:write"), s.requireMailUnlock).Post("/api/email-campaigns", s.createCampaign)
		r.With(s.perm("mail:email:write"), s.requireMailUnlock).Post("/api/email-messages/{id}/requeue", s.requeueEmailMessage)
		r.With(s.perm("mail:email:write"), s.requireMailUnlock).Post("/api/email-messages/{id}/abandon", s.abandonEmailMessage)
		// Drafts ride on the send permission: a draft only exists to become a
		// send, and every route is scoped to the caller inside the service.
		r.With(s.perm("mail:email:write"), s.requireMailUnlock).Post("/api/email-drafts", s.saveDraft)
		r.With(s.perm("mail:email:write"), s.requireMailUnlock).Get("/api/email-drafts", s.listDrafts)
		r.With(s.perm("mail:email:write"), s.requireMailUnlock).Get("/api/email-drafts/{id}", s.getDraft)
		r.With(s.perm("mail:email:write"), s.requireMailUnlock).Delete("/api/email-drafts/{id}", s.deleteDraft)
		r.With(s.perm("mail:email:write"), s.requireMailUnlock).Post("/api/email-drafts/{id}/send", s.sendDraft)

		// Timed delivery. Read sits behind the write permission, not the read
		// one: a scheduled send is nobody's correspondence yet, and the list
		// only ever shows the caller's own.
		r.With(s.perm("mail:email:write"), s.requireMailUnlock).Get("/api/email-scheduled", s.listScheduled)
		r.With(s.perm("mail:email:write"), s.requireMailUnlock).Post("/api/email-scheduled/{id}/send-now", s.sendScheduledNow)
		r.With(s.perm("mail:email:write"), s.requireMailUnlock).Post("/api/email-scheduled/{id}/cancel", s.cancelScheduled)
		r.With(s.perm("mail:email:read")).Get("/api/email-signatures", s.listSignatures)
		r.With(s.perm("mail:email:write")).Post("/api/email-signatures", s.createSignature)
		r.With(s.perm("mail:email:write")).Put("/api/email-signatures/{id}", s.updateSignature)
		r.With(s.perm("mail:email:write")).Delete("/api/email-signatures/{id}", s.deleteSignature)
		r.With(s.perm("mail:email:read")).Get("/api/email-templates", s.listEmailTemplates)
		r.With(s.perm("mail:email:write")).Post("/api/email-templates", s.createEmailTemplate)
		r.With(s.perm("mail:email:write")).Put("/api/email-templates/{id}", s.updateEmailTemplate)
		r.With(s.perm("mail:email:write")).Delete("/api/email-templates/{id}", s.deleteEmailTemplate)
		// The suppression list is shared by everybody's sends, so maintaining
		// it is administrative work rather than part of composing a mail.
		// Attachments and inline images. Uploading is part of composing, so
		// both ride on the send permission rather than a separate one.
		r.With(s.perm("mail:email:write")).Post("/api/email-attachments/presign", s.presignMailAttachment)
		r.With(s.perm("mail:email:write")).Post("/api/email-attachments", s.registerMailAttachment)
		r.With(s.perm("mail:email:read")).Get("/api/email-attachments", s.listMailAttachments)
		r.With(s.perm("mail:email:write")).Post("/api/email-images/presign", s.presignMailImage)
		r.With(s.perm("mail:email:write")).Post("/api/email-images", s.registerMailImage)
		r.With(s.perm("mail:email:write")).Post("/api/email-images/from-url", s.importMailImage)
		r.With(s.perm("mail:email:read")).Get("/api/email-images", s.listMailImages)
		r.With(s.perm("mail:email:write")).Delete("/api/email-images/{id}", s.withdrawMailImage)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/email-suppressions", s.listSuppressions)
		r.With(s.perm("mail:suppression:write")).Post("/api/email-suppressions", s.addSuppression)
		r.With(s.perm("mail:suppression:write")).Delete("/api/email-suppressions", s.removeSuppression)
		// The mail host is one setting for the whole company, so it sits
		// behind the administrative permission.
		r.With(s.perm("mail:email:read")).Get("/api/mail-host", s.getMailHost)
		r.With(s.perm("iam:role:write")).Put("/api/mail-host", s.saveMailHost)
		// Your own mailbox. Only :read is required to write it, because the
		// thing being written is your own credential — gating it behind an
		// administrative permission would mean an administrator has to be
		// involved in every employee entering their own authorisation code.
		// The caller's own inbox. Own mail only by construction — the
		// handler resolves the owner from the token, never from the request.
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/inbound-mails", s.listInbound)
		// Search sits beside the list rather than inside it: it crosses
		// folders, so it answers a different question and returns a
		// different shape.
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/mail-search", s.searchMail)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/inbound-mails/{id}", s.getInbound)
		// Word / Excel / PPT 的预览：转成 PDF 再看。POST 因为第一次真的会
		// 干活（转换并写进对象存储），之后是缓存命中。
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).
			Post("/api/inbound-mails/{id}/attachments/{attachmentId}/preview", s.previewInboundAttachment)
		// 一次把这封信的附件全下载下来。生产上 835 封信带 3 个以上附件，
		// 最多的一封 40 个，一个一个点不是办法。
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).
			Get("/api/inbound-mails/{id}/attachments/download", s.downloadInboundAttachments)
		// 邮箱转换也要能选择询盘模板；这里复用同一个只读 handler，但权限按
		// 邮箱场景收口，用户无需先获得采购模块权限或跳到采购页面。
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/mail-inquiry-templates", s.listInquiryTemplates)
		// Explicit user action only: selected text or one stored attachment is
		// sent to the configured model and returned as an Excel workbook.
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Post("/api/inbound-mails/{id}/excel", s.convertInboundToExcel)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/inbound-excel-jobs/{jobId}", s.getInboundExcelJob)
		// 智能转换的用量账（计量）。挂管理员的读权限而不是邮件读权限——
		// 这是账不是信，而且它跨全公司的人，不该谁能读自己的邮件谁就能看。
		// 也不需要邮箱解锁：它不含任何信件内容。
		r.With(s.perm("iam:employee:read")).Get("/api/excel-usage", s.excelUsage)

		// 平台开户（E7）。刻意没有 s.perm(...)：普通权限每家公司的超管都有，
		// 用它守平台等于没守。真正的守卫是 iam 里的 platform_operators 名单，
		// 每个 RPC 自查——网关只转发身份，名单说不行就是 403。
		r.Get("/api/platform/me", s.platformMe)
		r.Get("/api/platform/tenants", s.platformTenants)
		r.Post("/api/platform/tenants", s.platformCreateTenant)
		r.Post("/api/platform/tenants/{id}/reinvite", s.platformReinvite)
		r.Post("/api/platform/tenants/{id}/status", s.platformSetTenantStatus)
		// 死信运维面：守卫在 handler 里（平台操作员名单，判定在 iam），
		// 理由同上面平台开户的几条——不能用 s.perm，权限当门会向所有
		// 客户公司的超管敞开。
		r.Get("/api/platform/failed-events", s.listFailedEvents)
		r.Post("/api/platform/failed-events/replay", s.replayFailedEvent)
		// 智能转换的额度：定给客户公司的每月上限。同一道门，同一个理由——
		// 这是我们和客户之间的商务约定，不能让客户自己抬。
		r.Get("/api/platform/excel-quotas", s.listExcelQuotas)
		r.Post("/api/platform/excel-quotas", s.setExcelQuota)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Post("/api/inbound-mails/{id}/mark", s.markInbound)
		// Permanent deletion out of the trash. ERP-side copies only; the mail
		// host's original is beyond this API's reach by design.
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Delete("/api/inbound-mails/{id}", s.purgeInbound)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Post("/api/inbound-mails/mark-view-read", s.markViewRead)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Post("/api/inbound-mails/empty-trash", s.emptyTrash)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Post("/api/inbound-mails/empty-junk", s.emptyJunk)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/mail-threads", s.getMailThread)
		// Taking the conversation out of the system. Its own permission, and
		// still behind the mailbox gate: an export that skipped the gate
		// would be a way to read mail without passing it.
		r.With(s.perm("mail:email:export"), s.requireMailUnlock).
			Get("/api/mail-threads/export", s.exportMailThread)
		// The record of who did. Not behind the mailbox gate — this is
		// oversight of the mail module, not use of a mailbox, and the person
		// reading it may not have one bound.
		r.With(s.perm("mail:export:audit")).Get("/api/mail-exports", s.listMailExports)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Post("/api/mailbox/sync", s.syncMailbox)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/mailbox-sent", s.listMailboxSent)
		r.With(s.perm("mail:email:read")).Post("/api/mailbox/verify", s.verifyMailbox)
		r.With(s.perm("mail:email:read")).Get("/api/oauth/google/start", s.startGoogleOAuth)
		r.With(s.perm("mail:email:read")).Get("/api/mailbox/lock-status", s.mailLockStatus)
		// 退出**当前这一个**信箱：撤掉请求头里那把令牌，别的箱照开。
		r.With(s.perm("mail:email:read")).Post("/api/mailbox/lock", s.lockMailbox)
		// 全部退出：共用电脑走人时用。浏览器把手上所有令牌报上来一起撤。
		r.With(s.perm("mail:email:read")).Post("/api/mailbox/lock-all", s.lockAllMailboxes)
		// 读，不写凭据。**存邮箱凭据的路只有一条**：/api/mailbox/verify，
		// 而它要先拿这一对去邮件服务器真的登录一次，成功了才落库。
		//
		// 权限都是 mail:email:read 这一档，不是管理员档：员工写的是**自己
		// 的**信箱，挂成 iam:role:write 的话普通人点保存会收到一句通用的
		// 「没有权限」，看不出卡在哪一步。
		r.With(s.perm("mail:email:read")).Get("/api/my-mail-account", s.getMyMailAccount)
		r.With(s.perm("mail:email:read")).Get("/api/my-mailboxes", s.listMyMailboxes)
		r.With(s.perm("mail:email:read")).Post("/api/my-mailboxes/default", s.setDefaultMailbox)
		r.With(s.perm("mail:email:read")).Post("/api/my-mailboxes/unbind", s.unbindMailbox)
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
		// The header is looked at first and kept working: scripts, tests and
		// grpcurl-style tooling authenticate this way, and a header a foreign
		// page cannot set needs no CSRF proof. The browser's own path is the
		// cookie, which is where the CSRF obligation attaches.
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		fromCookie := false
		if raw == "" || raw == r.Header.Get("Authorization") {
			raw = ""
			if c, err := r.Cookie(SessionCookie); err == nil {
				raw, fromCookie = c.Value, true
			}
		}
		if raw == "" {
			s.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_MISSING", "缺少登录凭证")
			return
		}
		claims, err := authtoken.Parse(s.JWTSecret, raw)
		if err != nil {
			s.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "登录凭证无效或已过期")
			return
		}
		// Checked after the signature and before anything is done in this
		// person's name. A valid signature only says the token was minted by
		// us; it says nothing about whether somebody has since been shown the
		// door.
		//
		// Compared against the sign-in moment, not IssuedAt: renewal moves
		// IssuedAt forward, and comparing against a number the session can
		// advance by itself would let it renew past the cutoff.
		if s.Revocations != nil && s.Revocations.Revoked(
			claims.TenantID, claims.EmployeeID(), claims.SignedInAt()) {
			s.writeError(w, http.StatusUnauthorized, "AUTH_SESSION_REVOKED",
				"登录状态已被管理员终止，请重新登录")
			return
		}
		// The CSRF proof, demanded only where the risk exists: a cookie the
		// browser attaches on its own, carrying a state-changing request. A
		// GET reads nothing anybody couldn't read by phishing the person
		// directly, and a header-authenticated caller set that header itself
		// — no foreign page can.
		//
		// After the signature and revocation checks, so this answer is only
		// ever given about a session that is otherwise alive: a dead token
		// stays "请重新登录", never "请刷新页面".
		if fromCookie && mutating(r.Method) && !csrfOK(r) {
			s.writeError(w, http.StatusForbidden, "AUTH_CSRF_REQUIRED",
				"请求缺少防伪标记，请刷新页面后重试")
			return
		}
		// Only now — after the signature held and the session survived the
		// revocation check — is it safe to extend it. Renewing first would
		// hand a fresh token to a session that was about to be refused.
		s.renewIfHalfSpent(w, r, claims, fromCookie)
		ctx := grpcx.WithOperator(r.Context(), grpcx.Operator{
			TenantID:   claims.TenantID,
			EmployeeID: claims.EmployeeID(),
			Name:       claims.EmployeeName,
			Email:      claims.Email,
			// clientAddr, not RemoteAddr. Behind a reverse proxy RemoteAddr
			// is the proxy — every request in production logged as coming
			// from 127.0.0.1, which is the one value that answers no
			// question anybody asks of an audit trail. clientAddr honours
			// the forwarding header only when TRUST_PROXY_HEADERS says a
			// proxy that overwrites it is actually in front.
			IP:      clientAddr(r, s.TrustProxyHeaders),
			TraceID: newTraceID(),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// grpcMessage extracts the readable message from a gRPC error for surfaces
// that cannot render a JSON envelope, like a redirect query parameter.
func grpcMessage(err error) string {
	return status.Convert(err).Message()
}

// renewIfHalfSpent puts a fresh token on the response once the current one is
// past the midpoint of its life.
//
// A response header rather than an endpoint the client calls. The browser is
// already making the request; asking it to make a second one to stay signed in
// means every client has to remember to, and a background tab that forgets
// gets signed out while an identical tab beside it does not. Riding on traffic
// that already exists makes the rule "activity keeps you in" true by
// construction — and it is the same rule Frappe gets for free by writing a
// last-seen timestamp on every request.
//
// The effect of renewal-on-activity is that JWT_TTL stops being "how long
// since you signed in" and becomes "how long you may sit idle". Nothing else
// had to change for that: a session that keeps working keeps being extended,
// and one that stops simply runs out.
//
// Failure is silent on purpose. The request itself succeeded and the caller's
// current token is still valid for at least half its life; turning a renewal
// hiccup into a visible error would break a working page over something that
// will be retried on the very next request.
func (s *Server) renewIfHalfSpent(w http.ResponseWriter, r *http.Request, c *authtoken.Claims, fromCookie bool) {
	if !c.HalfSpent(time.Now()) {
		return
	}
	fresh, err := authtoken.Renew(s.JWTSecret, s.TokenTTL, c)
	if err != nil {
		s.Log.Warn("could not renew a session token", "employee", c.EmployeeID(), "err", err)
		return
	}
	// A cookie session renews as a cookie: Set-Cookie is the one channel no
	// script can observe, which is the point — the old design's renewal
	// header handed a fresh token to anything that had hooked XMLHttpRequest,
	// making renewal itself the leak that httpOnly storage had just closed.
	if fromCookie {
		s.refreshSessionCookies(w, r, fresh)
		return
	}
	// Same origin as the page, so no Access-Control-Expose-Headers is needed.
	// If the API is ever served from another origin, this header has to be
	// listed there or the browser will hide it and sessions will start
	// expiring under active use.
	w.Header().Set(RenewedTokenHeader, fresh)
}

// RenewedTokenHeader carries a replacement token to the browser.
const RenewedTokenHeader = "X-Renewed-Token"

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
	// Structured context from apierr: ids, limits, shortfalls. The message is
	// what a person reads; this is what the page can act on.
	Meta map[string]string `json:"meta,omitempty"`
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
	s.writeErrorMeta(w, httpStatus, code, msg, nil)
}

func (s *Server) writeErrorMeta(w http.ResponseWriter, httpStatus int, code, msg string, meta map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(envelope{Success: false, Code: code, Message: msg, Meta: meta})
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
		// "later", not "no". Without this a service saying somebody has run
		// out of attempts reaches the browser as a 500, and the page shows an
		// internal error instead of the wait it was told to report.
		codes.ResourceExhausted: http.StatusTooManyRequests,
	}[st.Code()]
	if httpCode == 0 {
		httpCode = http.StatusInternalServerError
	}
	bizCode := apierr.CodeFromStatus(err)
	if bizCode == "" {
		bizCode = "INTERNAL"
	}
	// Internal gRPC failures used to vanish after being translated into a
	// generic HTTP 500, leaving the browser with only Axios' "Request failed"
	// text and the service logs completely clean. Business refusals are normal
	// and stay quiet; unexpected failures must leave enough evidence to fix.
	if httpCode == http.StatusInternalServerError && s.Log != nil {
		s.Log.Error("upstream gRPC request failed", "grpc_code", st.Code().String(),
			"business_code", bizCode, "err", st.Message())
	}
	s.writeErrorMeta(w, httpCode, bizCode, st.Message(), apierr.MetaFromStatus(err))
}

// decodeBody parses a JSON request body directly into the gRPC request
// message, so REST and gRPC share one schema definition (the proto).
func (s *Server) decodeBody(w http.ResponseWriter, r *http.Request, msg proto.Message) bool {
	return s.decodeBodyLimit(w, r, msg, 1<<20)
}

// decodeBodyLimit 为确有大请求体需求的接口提供显式上限，避免扩大其他接口的攻击面。
func (s *Server) decodeBodyLimit(w http.ResponseWriter, r *http.Request, msg proto.Message, maxBytes int) bool {
	body, ok := s.readBodyLimit(w, r, maxBytes)
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
	return s.readBodyLimit(w, r, 1<<20)
}

func (s *Server) readBodyLimit(w http.ResponseWriter, r *http.Request, maxBytes int) ([]byte, bool) {
	body := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		body = append(body, buf[:n]...)
		if len(body) > maxBytes {
			s.writeError(w, http.StatusRequestEntityTooLarge, "GATEWAY_BODY_TOO_LARGE", "请求体过大")
			return nil, false
		}
		if err != nil {
			break
		}
	}
	return body, true
}
