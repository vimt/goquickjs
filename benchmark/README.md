# goquickjs benchmark

Cross-engine comparison for embeddable scripting runtimes in Go,
measuring three axes:

- **Time** — wall-clock per workload, median of N timed iterations.
- **Memory** — peak RSS during execution, net of the runtime baseline.
- **Binary size** — bytes a single embed of the engine adds to the
  hosting Go binary.

`goquickjs` is one entry among 13; this directory exists so the
numbers in the parent README are reproducible.

## Running

```sh
cd benchmark
go run .                                       # time, all engines, all benchmarks
go run . -mem                                  # also measure peak memory
go run . -size                                 # also measure binary-size delta
go run . -scale 3                              # 3× the work per benchmark
go run . -engines goquickjs,goja               # subset
go run . -benchmarks fib,sort                  # subset
go run . -n 10                                 # more iterations (more stable)
```

`-warmup` defaults to true (one untimed iteration first). `-n` sets
the timed iteration count (default 3, median reported).

## What's measured

Four small algorithms, hand-translated into every supported language
so the **return value matches across engines** — the harness fails
the run if any engine disagrees.

| benchmark | shape | stresses |
| --- | --- | --- |
| `arith` | loop with `Math.sqrt`/`floor` | tight numeric loops, builtin dispatch |
| `fib` | recursive Fibonacci | call-stack push/pop, frame creation |
| `sort` | bubble sort on a pre-seeded array | indexed read/write, comparisons |
| `strings` | concat + slice + `indexOf` in a loop | string allocation, builtin dispatch |

The reporter prints the **median** of N timed iterations and a
**geometric mean** across the 4 benchmarks (geomean is preferred over
arithmetic mean — `strings` is ~10× faster than the others on every
engine, so a plain mean is dominated by the slow workloads).

The harness creates a **fresh runtime per timed iteration** so any
engine that warms up state across calls doesn't get an unfair
advantage on later runs.

## Engines

13 engines across 6 languages compete:

| engine | language | kind |
| --- | --- | --- |
| **goquickjs** | JS | pure-Go (this repo) |
| goja | JS | pure-Go |
| sobek | JS | pure-Go (goja fork) |
| otto | JS | pure-Go |
| paserati | JS | pure-Go |
| quickjs (modernc.org) | JS | CGo binding to upstream QuickJS |
| v8 (tommie/v8go) | JS | CGo binding to V8 (omitted; see below) |
| gopher-lua | Lua | pure-Go |
| shopify-lua | Lua | pure-Go |
| tengo | Tengo | pure-Go |
| starlark | Starlark | pure-Go |
| risor | Risor | pure-Go |
| anko | Anko | pure-Go |
| yaegi | Go | pure-Go (interprets Go itself) |

## Test environment

- CPU: AMD Ryzen 7 8845HS (16 logical cores)
- RAM: 64 GiB
- OS: Linux 7.0.5 (Arch), x86_64
- Go: 1.26.3
- Date: 2026-06-21

## Results

### Time — scale=1, median of 5 iterations (ms, lower better)

```
engine       lang           arith        fib       sort    strings    geomean
-----------------------------------------------------------------------------
gopher-lua   [lua]         136.12      80.01      79.02       6.85       49.3
goquickjs    [js]          142.03     124.62     127.33       5.04       58.0  ← us
tengo        [tengo]       231.43     146.37     126.68       7.21       74.6
shopify-lua  [lua]         205.27     116.43      87.81      31.01       89.8
quickjs      [js]          207.43     155.73     180.03      14.39       95.6
yaegi        [go]          120.40     160.46      97.66      44.26       95.6
starlark     [starlark]    246.76     142.26     200.65      14.80      101.1
sobek        [js]          288.67     172.81     204.59      11.06      103.1
goja         [js]          300.07     169.37     210.29      11.05      104.2
anko         [anko]        888.79     732.66     794.68      65.49      429.1
risor        [risor]       345.25     187.42     185.28    2039.32      395.4
paserati     [js]          333.16     255.32     307.86    1892.42      471.8
otto         [js]         1081.21    1315.40    1498.88     499.70     1015.9
```

### Memory — peak RSS, net of Go runtime baseline (~25 MB)

```
engine       lang         peak(net)    work(net)
------------------------------------------------
quickjs      [js]             1.4MB        2.7MB   (CGo — most allocs in C heap)
goquickjs    [js]             6.0MB        5.7MB  ← #1 pure-Go JS
starlark     [starlark]        6.3MB        6.4MB
tengo        [tengo]          7.1MB        5.7MB
goja         [js]             8.6MB        7.4MB
anko         [anko]           8.8MB        6.8MB
gopher-lua   [lua]            9.1MB        6.6MB
sobek        [js]             9.4MB        7.4MB
otto         [js]            11.6MB        7.7MB
shopify-lua  [lua]           12.3MB        6.5MB
yaegi        [go]            15.3MB       10.2MB
risor        [risor]         24.5MB       29.5MB
paserati     [js]            30.3MB       20.6MB
```

`peak(net)` is `ru_maxrss` after the timed loop, minus the same
measurement on the engine-free baseline binary. `work(net)` is the
post-`runtime.GC()` resident set, approximating long-lived heap.

`quickjs` looks tiny because its strings, objects, and bytecode live
in C heap (`malloc`), which Go's `ru_maxrss` undercounts when the C
runtime returns pages to the OS. The CGo binding is genuinely lean —
~3 MB is real — but the gap to pure-Go engines is partly measurement
artifact.

### Binary size — bytes added by embedding one engine

```
engine       lang            binary        added
------------------------------------------------
engine-free baseline                       3.6MB

shopify-lua  [lua]            4.5MB       +0.9MB
gopher-lua   [lua]            4.8MB       +1.2MB
anko         [anko]           5.0MB       +1.4MB
starlark     [starlark]        5.0MB       +1.4MB
goquickjs    [js]             5.3MB       +1.7MB   ← #1 JS engine
tengo        [tengo]          5.4MB       +1.8MB
otto         [js]             7.6MB       +4.0MB
quickjs      [js]             8.2MB       +4.6MB
goja         [js]            13.9MB      +10.3MB
sobek        [js]            14.1MB      +10.5MB
risor        [risor]         14.8MB      +11.2MB
paserati     [js]            19.8MB      +16.2MB
yaegi        [go]            27.4MB      +23.8MB
```

`v8 (tommie/v8go)` is registered but skipped: CGo + static libv8 adds
~80 MB.

## Headline numbers (JS only)

| engine | time | memory | binary |
| --- | --- | --- | --- |
| **goquickjs** | **58.0 ms** | **6.0 MB** | **+1.7 MB** |
| quickjs (CGo) | 95.6 ms (1.65×) | 1.4 MB (0.23×)* | +4.6 MB (2.7×) |
| sobek | 103.1 ms (1.78×) | 9.4 MB (1.57×) | +10.5 MB (6.2×) |
| goja | 104.2 ms (1.80×) | 8.6 MB (1.43×) | +10.3 MB (6.1×) |
| paserati | 471.8 ms (8.13×) | 30.3 MB (5.05×) | +16.2 MB (9.5×) |
| otto | 1015.9 ms (17.5×) | 11.6 MB (1.93×) | +4.0 MB (2.4×) |

\* quickjs memory is undercounted (CGo); the real ratio is closer to
≈0.5×.

`goquickjs` is the **fastest pure-Go JS engine**, with the **smallest
binary footprint** of any JS engine measured, and the **lowest peak
memory of all pure-Go JS engines**. The CGo `quickjs` binding remains
attractive when memory matters and the cgo cost is acceptable.

## Caveats

- These workloads are CPU-bound numeric/string loops. They do not
  exercise large GC pressure, big-object handling, or string-builder
  fast paths — engines whose JIT excels at, e.g., array-heavy code
  may rank differently on real applications.
- `paserati` and `risor` collapse on `strings` because their concat
  implementation is O(n²) instead of amortised O(n); this dominates
  their geomean.
- All numbers are from one machine, one run. Pass `-n 10` for tighter
  bounds.
- `peak(net)` is approximate: `ru_maxrss` doesn't shrink when Go gives
  memory back; the per-process subprocess design means start-up cost
  shows up as a small floor on every engine.

## Why goquickjs is fast

The interpreter is unremarkable — a plain switch-dispatch over an
~80-op bytecode set. The wins come from:

1. **Shape-cached property layout** (V8/QuickJS hidden-class model):
   two objects that grow keys in the same order share a `*Shape`, so
   property reads compile to an array index, not a map lookup.
2. **24-byte tagged Value struct** (tag + float64 payload + ref
   pointer): numbers and booleans never escape to the heap; objects
   share a single pointer.
3. **Frame-stack VM with Lua-style upvalues**: closures capture by
   reference through a flat upvalue slot, avoiding the closure-cell
   indirection most pure-Go JS engines use.
4. **Native fast paths for common builtins**: `Array.prototype.push`,
   `String.prototype.indexOf`, `Math.*` etc. dispatch directly through
   Go functions rather than re-entering the VM.
5. **Pooled frame locals + lazy `arguments`**: recycled across calls,
   skip the per-call malloc + zero. `arguments` Array is only built
   when the body actually reads the binding.
6. **In-place binary ops**: `OpAdd/Sub/Mul/Div/Mod/Pow` and comparison
   opcodes mutate the value stack in place rather than `pop/pop/push`,
   so tight arithmetic loops skip three closure calls per op.
7. **Integer fast path on `%`**: `jsModFast` uses native `int64 %`
   when both sides round-trip cleanly through `int64`, avoiding
   `math.Mod`'s frexp/abs hot loop.

There's no JIT, no register allocation, no method inline cache. Most
of the remaining headroom is in those.
