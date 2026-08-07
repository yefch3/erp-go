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
	IAM          iamv1.AuthServiceClient
	Directory    iamv1.DirectoryServiceClient
	Access       iamv1.AccessServiceClient
	Customers    mdv1.CustomerServiceClient
	Suppliers    mdv1.SupplierServiceClient
	Options      mdv1.OptionServiceClient
	Numbering    mdv1.NumberingServiceClient
	Fx           fxv1.FxServiceClient
	Approval     apv1.ApprovalServiceClient
	Catalog      pdv1.CatalogServiceClient
	Attachments  pdv1.AttachmentServiceClient
	Attributes   pdv1.AttributeServiceClient
	Quotations   exv1.QuotationServiceClient
	Contracts    exv1.ContractServiceClient
	Shipments    exv1.ShipmentServiceClient
	Receipts     exv1.ReceiptServiceClient
	Requirements prv1.RequirementServiceClient
	Orders       prv1.PurchaseOrderServiceClient
	Stocks       ivv1.StockServiceClient
	Shipping     shippingv1.ShippingServiceClient
	Emails       mailv1.EmailServiceClient
	// Unlock holds mailbox-verification tokens. Nil fails closed: every mail
	// route answers MAIL_LOCKED until a store exists.
	Unlock *UnlockStore
	// Throttle counts failed logins and failed mailbox verifications. Nil is
	// allowed and fails open — see FailureThrottle for why this one is the
	// other way round from Unlock.
	Throttle *FailureThrottle
	// Google OAuth. The client id is public by design; the secret lives only
	// in the notification service, which does the token exchange.
	GoogleClientID   string
	OAuthRedirectURL string
	FrontendBaseURL  string
	Live             *livefeed.Subscriber
	JWTSecret        string
	Log              *slog.Logger
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	// Without this a handler panic drops the connection, which reaches the
	// user as "network error" instead of a readable failure.
	r.Use(s.recoverPanics)
	r.Post("/api/auth/login", s.login)
	// Activation. Login-free of necessity: whoever holds the link has no
	// account yet, and getting one is the point. The token in it is the whole
	// of their claim — see activateAccount for why that is enough.
	r.Get("/api/auth/invitation", s.peekInvitation)
	r.Post("/api/auth/activate", s.activateAccount)
	// Images embedded in sent mail. Public by necessity: the fetcher is the
	// recipient's mail client, which has no session. See serveMailImage.
	r.Get("/api/public/mail-images/{token}", s.serveMailImage)
	// The open-tracking pixel. Also login-free, and also deliberately
	// indistinguishable between a real key and a made-up one.
	r.Get("/api/public/mail-open/{key}", s.serveOpenPixel)
	// Google sends the browser back here after its own login page. State is
	// the authentication; see googleOAuthCallback.
	r.Get("/api/oauth/google/callback", s.googleOAuthCallback)
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
		// Sending the invitation is employee administration, so it carries the
		// same permission as creating the row it invites.
		r.With(s.perm("iam:employee:write")).Post("/api/employees/{id}/invite", s.inviteEmployee)
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
		r.With(s.perm("export:contract:write")).Post("/api/contracts/direct", s.createDirectContract)
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
		// Which boats one contract's goods are on. Gated on reading shipments,
		// not contracts: it is shipping information reached from the other end.
		r.With(s.perm("export:shipment:read")).Get("/api/contracts/{id}/vessels", s.listContractVessels)
		r.With(s.perm("shipping:schedule:read")).Get("/api/shipping/status", s.getShippingStatus)
		r.With(s.perm("shipping:schedule:read")).Get("/api/shipping/schedules", s.listShippingSchedules)
		r.With(s.perm("shipping:schedule:read")).Get("/api/shipping/statistics", s.getShippingStatistics)
		r.With(s.perm("shipping:schedule:read")).Get("/api/shipping/schedules/{id}", s.getShippingSchedule)
		r.With(s.perm("shipping:schedule:write")).Post("/api/shipping/schedules", s.createShippingSchedule)
		r.With(s.perm("shipping:schedule:write")).Put("/api/shipping/schedules/{id}", s.updateShippingSchedule)
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
		r.With(s.perm("export:receipt:read")).Get("/api/bank-transactions", s.listTransactions)
		r.With(s.perm("export:receipt:read")).Get("/api/bank-transactions/{id}", s.getTransaction)
		r.With(s.perm("export:receipt:write")).Post("/api/bank-transactions", s.recordTransaction)
		r.With(s.perm("export:receipt:write")).Post("/api/bank-transactions/{id}/allocate", s.allocateReceipt)
		r.With(s.perm("export:receipt:write")).Post("/api/bank-transactions/{id}/irrelevant", s.markTransactionIrrelevant)
		r.With(s.perm("export:receipt:write")).Post("/api/bank-transactions/{id}/reopen", s.reopenTransaction)
		// Reversal, not deletion: there is no DELETE route here on purpose.
		r.With(s.perm("export:receipt:write")).Post("/api/receipt-allocations/{id}/reverse", s.reverseAllocation)
		r.With(s.perm("export:receipt:read")).Get("/api/open-receivables", s.listOpenReceivables)
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
		r.With(s.perm("inventory:stock:read")).Get("/api/stocks", s.listStocks)
		r.With(s.perm("inventory:stock:read")).Get("/api/stock-ledger", s.listStockLedger)
		r.With(s.perm("inventory:stock:write")).Post("/api/stocks/receive", s.receiveStock)
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
		r.With(s.perm("procurement:requirement:read")).Get("/api/requirements/{id}", s.getRequirement)
		r.With(s.perm("procurement:requirement:write")).Post("/api/requirements", s.createRequirement)
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
		r.With(s.perm("procurement:order:write")).Post("/api/purchase-orders", s.createOrder)
		r.With(s.perm("procurement:order:write")).Post("/api/purchase-orders/{id}/submit", s.submitOrder)
		r.With(s.perm("procurement:order:write")).Post("/api/purchase-orders/{id}/cancel", s.cancelOrder)
		// Receiving is warehouse work, so it rides on the stock permission
		// rather than the buyer's.
		r.With(s.perm("inventory:stock:write")).Post("/api/purchase-orders/{id}/receive", s.receiveOrder)
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
		// Correspondence. Reading is scoped by the notification data scope —
		// the permission only says "may open the mail module at all", the
		// scope decides whose mail comes back.
		// The address book the composer picks from. Gated on sending: it is
		// only ever used to choose who a mail goes to.
		r.With(s.perm("mail:email:write")).Get("/api/mailing-contacts", s.listMailingContacts)
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
		r.With(s.perm("mail:email:write")).Delete("/api/email-signatures/{id}", s.deleteSignature)
		// The suppression list is shared by everybody's sends, so maintaining
		// it is administrative work rather than part of composing a mail.
		// Attachments and inline images. Uploading is part of composing, so
		// both ride on the send permission rather than a separate one.
		r.With(s.perm("mail:email:write")).Post("/api/email-attachments/presign", s.presignMailAttachment)
		r.With(s.perm("mail:email:write")).Post("/api/email-attachments", s.registerMailAttachment)
		r.With(s.perm("mail:email:read")).Get("/api/email-attachments", s.listMailAttachments)
		r.With(s.perm("mail:email:write")).Post("/api/email-images/presign", s.presignMailImage)
		r.With(s.perm("mail:email:write")).Post("/api/email-images", s.registerMailImage)
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
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Post("/api/inbound-mails/{id}/mark", s.markInbound)
		// Permanent deletion out of the trash. ERP-side copies only; the mail
		// host's original is beyond this API's reach by design.
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Delete("/api/inbound-mails/{id}", s.purgeInbound)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Post("/api/inbound-mails/mark-view-read", s.markViewRead)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Post("/api/inbound-mails/empty-trash", s.emptyTrash)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Post("/api/inbound-mails/empty-junk", s.emptyJunk)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/mail-threads", s.getMailThread)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Post("/api/mailbox/sync", s.syncMailbox)
		r.With(s.perm("mail:email:read"), s.requireMailUnlock).Get("/api/mailbox-sent", s.listMailboxSent)
		r.With(s.perm("mail:email:read")).Post("/api/mailbox/verify", s.verifyMailbox)
		r.With(s.perm("mail:email:read")).Get("/api/oauth/google/start", s.startGoogleOAuth)
		r.With(s.perm("mail:email:read")).Get("/api/mailbox/lock-status", s.mailLockStatus)
		r.With(s.perm("mail:email:read")).Post("/api/mailbox/lock", s.lockMailbox)
		// Read-only on purpose: there is no API that stores a mailbox
		// credential directly. The only way in is /api/mailbox/verify with an
		// address — a live login at the mail host that stores the pair only
		// after it succeeded.
		r.With(s.perm("mail:email:read")).Get("/api/my-mail-account", s.getMyMailAccount)
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
			Email:      claims.Email,
			IP:         r.RemoteAddr,
			TraceID:    newTraceID(),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// grpcMessage extracts the readable message from a gRPC error for surfaces
// that cannot render a JSON envelope, like a redirect query parameter.
func grpcMessage(err error) string {
	return status.Convert(err).Message()
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
	}[st.Code()]
	if httpCode == 0 {
		httpCode = http.StatusInternalServerError
	}
	bizCode := apierr.CodeFromStatus(err)
	if bizCode == "" {
		bizCode = "INTERNAL"
	}
	s.writeErrorMeta(w, httpCode, bizCode, st.Message(), apierr.MetaFromStatus(err))
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
