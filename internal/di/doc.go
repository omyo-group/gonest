// Package di implements core.Container: constructor-based dependency
// injection resolved by type, with singleton and transient scopes, parameter
// objects (core.In), named providers, cycle detection and eager validation.
//
// Register analyses every constructor once and stores an injection plan.
// Resolve(tok, from) selects the provider of tok seen from module from: the
// provider owned by from first, else the single module provider allowed by the
// Visibility rule, else a framework provider (nil owner). Dependencies of a
// provider are resolved as seen from its owner module. Validate resolves every
// provider so that a broken graph fails at startup, reporting each distinct
// problem once. After a singleton is built, resolving it again is a single map
// lookup under a read lock with no allocation.
//
// Errors from Resolve, Validate and Invoke's injection step are
// *core.ResolveError values with the full dependency path and one of the
// reasons "no provider", "not visible from module x", "ambiguous", "cycle" or
// "constructor failed", plus a hint when one can be computed.
//
// Owner: agent A1.
package di
