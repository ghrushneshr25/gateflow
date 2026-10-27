package authn

// Scope constants (dev HMAC JWT). Keep stable; add new scopes rather than overloading "*".
const (
	ScopeWildcard = "*"

	ScopeSaleWrite   = "ops:sale:write"
	ScopeSalePublish = "ops:sale:publish"
	// ScopeUsersAdmin allows creating/listing operator users (sale auth API).
	ScopeUsersAdmin = "ops:users:write"

	ScopeInventoryWrite = "inventory:write"

	ScopeAdmissionGrant       = "admission:grant"
	ScopeAdmissionEntitlement = "admission:entitlement"
	ScopeAdmissionRead        = "ops:admission:read"
	ScopeAdmissionFence       = "ops:admission:fence"
	ScopeAdmissionBudget      = "ops:admission:budget"

	ScopeOrderRead    = "ops:order:read"
	ScopeOrderRecover = "ops:order:recover"
	ScopeOutboxRead   = "ops:outbox:read"
	ScopeOutboxReplay = "ops:outbox:replay"

	ScopePaymentCreate   = "payment:create"
	ScopePaymentCallback = "payment:callback"
	ScopePaymentRecover  = "ops:payment:recover"

	// Buyer product-path scope (checkout, own orders, queue).
	ScopeBuyerSelf = "buyer:self"
)

// Principal types.
const (
	PrincipalBuyer    = "buyer"
	PrincipalOperator = "operator"
	PrincipalService  = "service"
)

// DefaultDevSecret is the well-known local development HMAC secret. Never use in production.
const DefaultDevSecret = "gateflow-dev-secret-change-me"

// OrderServiceScopes is the least-privilege set minted for the order service S2S principal.
var OrderServiceScopes = []string{
	ScopeInventoryWrite,
	ScopeAdmissionGrant,
	ScopeAdmissionEntitlement,
	ScopePaymentCreate,
	ScopePaymentRecover,
	ScopeOrderRecover,
}

// SaleServiceScopes is minted for sale→inventory pool create.
var SaleServiceScopes = []string{
	ScopeInventoryWrite,
}
