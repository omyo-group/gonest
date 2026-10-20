// Package tenants is the multi-tenancy module of the SaaS example. A gin
// middleware, bound with Module.Configure, resolves the tenant from the
// X-Tenant-ID header; Service is exported for other modules; MemberGuard
// checks that the authenticated user belongs to that tenant.
package tenants
