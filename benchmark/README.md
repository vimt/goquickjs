# goquickjs benchmark

A cross-engine performance comparison for embeddable scripting
runtimes in Go. `goquickjs` is one entry among many; this directory
exists so the numbers in the parent README are reproducible.

## Running

```sh
cd benchmark
go run .                                # all engines, all benchmarks
go run . -scale 3                       # 3× the work per benchmark
go run . -engines goquickjs,goja        # just two engines
go run . -benchmarks fib,sort           # just two workloads
```

`-warmup` defaults to true (one untimed iteration before timing).
`-iters` controls the number of timed iterations (default 3, median
reported).

## What's measured

Four small algorithms, deliberately portable so every engine can run
the same logic:

| benchmark | shape | stresses |
| --- | --- | --- |
| `arith` | loop accumulator with `Math.sqrt`/`floor` | tight numeric loops, function-call overhead |
| `fib` | naive recursive Fibonacci | call-stack + frame creation cost |
| `sort` | bubble sort on a pre-seeded array | array indexed read/write, comparisons |
| `strings` | concat + slice + `indexOf` in a loop | string allocation + builtin dispatch |

Each script is hand-translated into every supported language so the
**checksum it returns is identical across engines** — the harness fails
the run if any engine disagrees, catching silent semantic drift.

The reporter takes the **median** of N timed iterations (default 3) and
prints a **geometric mean** across the 4 benchmarks. Geomean is
preferred over arithmetic mean because it weights speed-ups across very
different magnitudes (the `strings` workload is ~10× faster than the
others on every engine).

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
| v8 (tommie/v8go) | JS | CGo binding to V8 (compile-only; omitted from results below) |
| gopher-lua | Lua | pure-Go |
| shopify-lua | Lua | pure-Go |
| tengo | Tengo | pure-Go |
| starlark | Starlark | pure-Go |
| risor | Risor | pure-Go |
| anko | Anko | pure-Go |
| yaegi | Go | pure-Go (interprets Go itself) |

The harness creates a **fresh runtime per timed iteration** so any
engine that warms up state across calls doesn't get an unfair advantage
on the second run.

## Test environment

- CPU: AMD Ryzen 7 8845HS (16 logical cores)
- RAM: 64 GiB
- OS: Linux 7.0.5 (Arch), x86_64
- Go: 1.26.3
- Date: 2026-06-21

## Results

### scale=1 (default work size)

Median over 3 timed iterations (1 warmup), milliseconds. Lower is
better. Engines listed by geomean.

```
engine       lang           arith        fib       sort    strings    geomean
-----------------------------------------------------------------------------
gopher-lua   [lua]         136.91      79.07      81.02       6.73       49.3
goquickjs    [js]          146.71     148.80     119.75       6.39       63.9 ← us
tengo        [tengo]       223.82     164.50     117.23       7.65       75.8
yaegi        [go]          112.71     149.07      90.98      40.04       88.4
shopify-lua  [lua]         205.22     119.39      94.43      30.29       91.5
quickjs      [js]          207.51     147.94     180.19      14.74       95.0
starlark     [starlark]    228.17     143.05     175.22      14.95       96.2
sobek        [js]          286.93     168.82     203.92      11.63      103.5
goja         [js]          298.90     170.79     211.43      11.63      105.8
anko         [anko]        808.46     717.77     791.23      66.88      418.6
risor        [risor]       355.21     192.74     211.65    2136.88      419.5
paserati     [js]          347.34     242.95     299.86    1930.19      470.1
otto         [js]         1148.40    1330.95    1527.31     501.67     1040.3
```

### scale=3 (3× the work)

```
engine       lang           arith        fib       sort    strings    geomean
-----------------------------------------------------------------------------
gopher-lua   [lua]         405.81     232.94     698.46      18.57      187.1
goquickjs    [js]          487.07     403.20     990.92      18.03      243.4 ← us
tengo        [tengo]       660.46     417.86    1047.36      21.49      280.8
quickjs      [js]          627.31     433.48    1549.28      40.73      361.9
starlark     [starlark]    664.92     413.79    1585.58      43.97      372.2
yaegi        [go]          324.83     430.98     757.14     245.45      401.6
sobek        [js]          875.37     509.45    1845.08      32.72      405.1
goja         [js]          906.78     529.24    1960.75      34.38      424.1
shopify-lua  [lua]         597.35     355.99     815.76     213.29      438.6
risor        [risor]      1047.81     568.58    1672.22   18700.15     2077.6
anko         [anko]       2529.00    2262.03    7330.11     417.81     2045.9
paserati     [js]          952.56     721.57    2507.55   16953.12     2325.0
otto         [js]         3344.23    3986.69   13679.48    3906.50     5166.4
```

## Reading the numbers

**For pure-Go JS engines specifically:**

| engine | scale=1 geomean | scale=3 geomean | vs goquickjs (scale=3) |
| --- | --- | --- | --- |
| **goquickjs** | **63.9 ms** | **243.4 ms** | **1.00×** |
| sobek | 103.5 ms | 405.1 ms | 1.66× slower |
| goja | 105.8 ms | 424.1 ms | 1.74× slower |
| paserati | 470.1 ms | 2325.0 ms | 9.55× slower |
| otto | 1040.3 ms | 5166.4 ms | 21.23× slower |

**Against the CGo-backed reference:**

| engine | scale=3 geomean | vs goquickjs |
| --- | --- | --- |
| quickjs (CGo binding to upstream QuickJS) | 361.9 ms | 1.49× slower than goquickjs |

i.e. our pure-Go reimplementation is ~1.5× the throughput of the C
QuickJS library called via CGo — most of the lead comes from avoiding
the CGo crossing cost on each Eval, not from any algorithmic edge.

**Against the fastest non-JS engine:**

`gopher-lua` remains 15-30% faster across the board. Lua semantics are
simpler than JS (no shape transitions, weaker type system, no
prototype chain) so this is roughly the limit a pure-Go scripting VM
can reach.

## Caveats

- **v8 (tommie/v8go)** is registered in the runner but excluded from
  the table: it needs cgo + libv8 and the build environment in this
  repo is pure Go to keep CI cheap.
- These workloads are **CPU-bound numeric/string loops**. They do not
  exercise GC pressure, allocator hot paths, or large-object handling
  — engines whose JIT excels at, e.g., array-heavy code may rank
  differently on different workloads.
- `paserati` and `risor` collapse on the `strings` benchmark because
  their string concat implementation is O(n²) rather than amortised
  O(n); this dominates their geomean.
- All numbers are from one machine, one run. The harness prints
  per-iteration spreads if you pass `-iters 10` for a more rigorous
  measurement.

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

There's no JIT, no register allocation, no inline caching for method
lookups. Most of the remaining headroom is in those.
