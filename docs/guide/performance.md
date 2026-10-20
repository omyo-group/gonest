# Performance

Gonest does all discovery work at startup: walking modules, analysing
constructors, building singletons, parsing struct tags, compiling one input
binder per route and composing the guard/interceptor/pipe/filter chain into
one closure per route. A request then runs:

- the Gin router and the route's precomputed middleware chain;
- one `*gonest.Ctx` allocation;
- guards, interceptors, pipes from pre-built slices;
- the compiled binder (reflection over a precomputed field plan, no tag
  parsing), then go-playground/validator if the input has `validate` tags;
- the handler and `gin.Context.JSON`.

There is no container lookup and no map lookup by type per request.

## Benchmark

`examples/bench/bench_test.go` serves the same endpoints with raw Gin
handlers written by hand and with Gonest, and checks that both return the
same bodies (`TestEndpointsAgree`). Requests are served in-process through
`httptest.NewRecorder`, so the numbers include Gin routing, the recorder and
JSON encoding, but no network.

| Scenario | Raw Gin | Gonest |
|---|---|---|
| NoInput | `c.JSON(200, item)` | `Handle(func(*Ctx, Empty) (Item, error))` |
| PathQuery | `strconv.Atoi(c.Param("id"))`, manual `id < 1` check, `c.Query("verbose")` | input struct `path:"id" validate:"min=1"`, `query:"verbose"` |
| PathQueryPtr | (compare with PathQuery) | same with `Handle[*In, Out]` |
| PathQueryNoValidatePtr | (compare with PathQuery) | `*In` without `validate` tags |
| GuardInterceptor | auth middleware + handler | route guard + pass-through interceptor + handler |
| JSONBody | `ShouldBindJSON` + `binding:"required"` | `json:"name" validate:"required"` |

### Results

Go 1.24.7, linux/amd64, Intel Xeon @ 2.10GHz, 2 vCPUs shared with other
jobs, `GOMAXPROCS=1`, Gin in release mode. Twelve interleaved rounds of
200,000 iterations each; the table gives the median and the minimum of the
12 ns/op values. Run-to-run noise on this machine is about ±15%: compare
pairs, not single numbers.

| Scenario | Gin median | Gonest median | Gin min | Gonest min | Overhead (min) | B/op Gin → Gonest | allocs Gin → Gonest |
|---|---|---|---|---|---|---|---|
| NoInput | 1452 ns | 1573 ns | 1240 ns | 1379 ns | +11% (+139 ns) | 1088 → 1120 | 11 → 12 |
| PathQuery | 1878 ns | 2800 ns | 1700 ns | 2188 ns | +29% (+488 ns) | 1504 → 1568 | 14 → 17 |
| PathQueryPtr | 1878 ns | 3034 ns | 1700 ns | 2251 ns | +32% (+551 ns) | 1504 → 1552 | 14 → 16 |
| PathQueryNoValidatePtr | 1878 ns | 2300 ns | 1700 ns | 1894 ns | +11% (+194 ns) | 1504 → 1552 | 14 → 16 |
| GuardInterceptor | 1990 ns | 2702 ns | 1721 ns | 2405 ns | +40% (+684 ns) | 1504 → 1656 | 14 → 19 |
| JSONBody | 3077 ns | 3137 ns | 2540 ns | 2871 ns | +13% (+331 ns) | 2072 → 2120 | 21 → 23 |

### Reading the numbers

- **The pipeline itself** (NoInput) costs one allocation, the 32-byte
  `*gonest.Ctx`, plus about 100 to 150 ns.
- **Binding** path and query values by reflection costs about 200 ns over
  hand-written `strconv` calls (PathQueryNoValidatePtr).
- **Validation** is the largest item when it is used: running
  go-playground/validator on a struct (PathQuery vs
  PathQueryNoValidatePtr) costs about 300 ns, while the raw Gin handler only
  does `id < 1`. Inputs without `validate` tags skip the validator entirely.
  For JSON bodies both sides run the validator, and the difference shrinks to
  about 10%.
- **Guards and interceptors** are interface calls over slices; the
  interceptor chain allocates the `next` closures, hence +5 allocations
  against a plain Gin middleware.
- `Handle[*In, Out]` saves one allocation; in time it is within the noise.

The design target in `CLAUDE.md` is under 5% overhead against equivalent raw
Gin. On these micro-endpoints, which do no work at all, Gonest is **not**
within 5%: it costs between about 0.1 µs (no input) and 0.7 µs (guard +
interceptor + validated input) per request, a constant that does not grow
with the handler. For a handler that does any I/O (a database round trip is
typically 100 µs or more), that constant is well under 1%. A2's handoff
(`docs/handoff/a2.md`) has a breakdown of the pipeline benchmarks at the
package level.

### Reproducing

```sh
GOMAXPROCS=1 go test -run '^$' -bench . -benchmem -count 6 ./examples/bench/
go test ./examples/bench/        # checks both stacks return the same bodies
```

On a shared machine, build the test binary once and alternate rounds, which
is how the table above was produced:

```sh
go test -c -o bench.test ./examples/bench/
for i in $(seq 12); do
  GOMAXPROCS=1 ./bench.test -test.run '^$' -test.bench . -test.benchmem -test.benchtime 200000x
done
```
