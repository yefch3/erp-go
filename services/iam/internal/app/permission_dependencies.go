package app

import "sort"

// permissionDependencies is the server-side contract between an operation
// and the page/read capability needed to reach that operation.  A role that
// may edit, approve or send a record but cannot read the corresponding page
// is not useful: the browser hides the entry and the gateway rejects the
// page's initial GET before the user can reach the allowed button.
//
// Keep the exceptional cross-resource relationships explicit.  In
// particular quality:task:request intentionally does not imply
// quality:task:read: buyers request an inspection from a purchase order, but
// only quality staff may enter the quality workbench.
var permissionDependencies = map[string][]string{
	"approval:flow:write": {"approval:flow:read"},

	"export:contract:approve":   {"export:contract:read"},
	"export:contract:write":     {"export:contract:read"},
	"export:ownership:transfer": {"export:contract:read"},
	"export:quotation:write":    {"export:quotation:read"},
	"export:receipt:write":      {"export:receipt:read"},
	"export:shipment:write":     {"export:shipment:read"},

	"fx:rate:write": {"fx:rate:read"},

	"iam:department:write": {"iam:department:read"},
	"iam:employee:write":   {"iam:employee:read"},
	"iam:role:write":       {"iam:role:read"},

	"inventory:stock:cost":   {"inventory:stock:read"},
	"inventory:stock:import": {"inventory:stock:read"},
	"inventory:stock:write":  {"inventory:stock:read"},

	"mail:email:export":      {"mail:email:read"},
	"mail:email:write":       {"mail:email:read"},
	"mail:supervision:audit": {"mail:supervision:read"},
	"mail:suppression:write": {"mail:email:read"},

	"masterdata:customer:write": {"masterdata:customer:read"},
	"masterdata:factory:write":  {"masterdata:factory:read"},
	"masterdata:port:write":     {"masterdata:port:read"},
	"masterdata:supplier:write": {"masterdata:supplier:read"},

	"procurement:exception:write":       {"procurement:order:read"},
	"procurement:invoice:write":         {"procurement:invoice:read"},
	"procurement:order:cancel":          {"procurement:order:read"},
	"procurement:order:close":           {"procurement:order:read"},
	"procurement:order:send":            {"procurement:order:read"},
	"procurement:order:submit":          {"procurement:order:read"},
	"procurement:order:write":           {"procurement:order:read"},
	"procurement:payment:write":         {"procurement:payment:read"},
	"procurement:production:write":      {"procurement:order:read"},
	"procurement:receipt:write":         {"procurement:order:read"},
	"procurement:recon:write":           {"procurement:recon:read", "procurement:order:read"},
	"procurement:requirement:exception": {"procurement:requirement:read"},
	"procurement:requirement:write":     {"procurement:requirement:read"},
	"procurement:sourcing:approve":      {"procurement:sourcing:read"},
	"procurement:sourcing:price":        {"procurement:sourcing:read"},
	"procurement:sourcing:send":         {"procurement:sourcing:read"},
	"procurement:sourcing:write":        {"procurement:sourcing:read"},

	"product:product:write": {"product:product:read"},

	"quality:file:upload":  {"quality:task:read"},
	"quality:task:request": {"procurement:order:read"},
	"quality:task:write":   {"quality:task:read"},

	"sales:inquiry:submit": {"sales:inquiry:read"},
	"sales:inquiry:write":  {"sales:inquiry:read"},
	"sales:inquiry:delete": {"sales:inquiry:read"},

	"shipping:document:download":   {"shipping:document:view", "shipping:schedule:read"},
	"shipping:document:invalidate": {"shipping:document:view", "shipping:schedule:read"},
	"shipping:document:manage":     {"shipping:document:view", "shipping:schedule:read"},
	"shipping:document:upload":     {"shipping:document:view", "shipping:schedule:read"},
	"shipping:document:view":       {"shipping:schedule:read"},
	"shipping:progress:write":      {"shipping:schedule:read"},
	"shipping:route:write":         {"shipping:schedule:read"},
	"shipping:schedule:write":      {"shipping:schedule:read"},
	"shipping:sourcing:approve":    {"shipping:sourcing:read"},
	"shipping:sourcing:write":      {"shipping:sourcing:read"},
}

// expandPermissionCodes returns a stable, duplicate-free transitive closure.
// Unknown requested codes are kept so the caller can reject them; a missing
// dependency in the catalogue is not invented and is caught by the catalogue
// consistency test instead.
func expandPermissionCodes(codes []string, known map[string]bool) []string {
	granted := make(map[string]bool, len(codes))
	queue := append([]string(nil), codes...)
	for len(queue) > 0 {
		code := queue[0]
		queue = queue[1:]
		if granted[code] {
			continue
		}
		granted[code] = true
		for _, required := range permissionDependencies[code] {
			if known[required] && !granted[required] {
				queue = append(queue, required)
			}
		}
	}
	out := make([]string, 0, len(granted))
	for code := range granted {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}
