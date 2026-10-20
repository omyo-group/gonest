// Package bench compares Gonest endpoints with hand-written raw Gin handlers
// doing the same work. It contains only benchmarks and tests:
//
//	GOMAXPROCS=1 go test -run '^$' -bench . -benchmem -count 6 ./examples/bench/
//
// Results are recorded in docs/guide/performance.md.
package bench
