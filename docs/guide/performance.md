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
- the handler, then the result encoded with Gin's JSON codec and written.

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
| NoInput | 1340 ns | 1416 ns | 1271 ns | 1276 ns | +0% (+5 ns) | 1088 → 1104 | 11 → 11 |
| PathQuery | 1922 ns | 2497 ns | 1633 ns | 2124 ns | +30% (+491 ns) | 1504 → 1536 | 14 → 15 |
| PathQueryPtr | 1922 ns | 2537 ns | 1633 ns | 2168 ns | +33% (+535 ns) | 1504 → 1536 | 14 → 15 |
| PathQueryNoValidatePtr | 1922 ns | 2196 ns | 1633 ns | 1901 ns | +16% (+268 ns) | 1504 → 1536 | 14 → 15 |
| GuardInterceptor | 2019 ns | 2730 ns | 1743 ns | 2379 ns | +36% (+636 ns) | 1504 → 1624 | 14 → 17 |
| JSONBody | 3083 ns | 3280 ns | 2755 ns | 2761 ns | +0% (+6 ns) | 2072 → 2088 | 21 → 21 |

(Measured after the v0.1 review fixes: the response is encoded with gin's
JSON codec and written directly, which avoids the boxed `render.JSON` value
and pays for the `*gonest.Ctx`; value inputs are bound into a recycled
scratch struct.)

### Reading the numbers

- **The pipeline itself** (NoInput) allocates the 32-byte `*gonest.Ctx`
  but saves the boxed renderer gin's `c.JSON` allocates, so it is level
  with raw Gin in allocations and within the noise in time.
- **Binding** path and query values by reflection costs about 200 ns over
  hand-written `strconv` calls (PathQueryNoValidatePtr).
- **Validation** is the largest item when it is used: running
  go-playground/validator on a struct (PathQuery vs
  PathQueryNoValidatePtr) costs about 300 ns, while the raw Gin handler only
  does `id < 1`. Inputs without `validate` tags skip the validator entirely.
  For JSON bodies both sides run the validator, and the difference shrinks to
  about 10%.
- **Guards and interceptors** are interface calls over slices; the
  the interceptor chain allocates its per-request state and the `next`
  closures (+2 allocations against a plain Gin middleware).
- `Handle[*In, Out]` saves one allocation; in time it is within the noise.

The design target in `CLAUDE.md` is under 5% overhead against equivalent raw
Gin. On these micro-endpoints, which do no work at all, Gonest is **not**
within 5% once an input is bound and validated: it costs from nothing
measurable (no input, JSON body) to about 0.6 µs (guard + interceptor +
validated input) per request, mostly the validator, a constant that does not grow
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
