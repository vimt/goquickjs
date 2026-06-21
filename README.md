# goquickjs

A pure-Go JavaScript engine, built from scratch by AI.

```go
import "github.com/vimt/goquickjs"

v, err := goquickjs.Eval(`
  function fib(n) { return n < 2 ? n : fib(n-1) + fib(n-2); }
  fib(10)
`)
// v == "55"
```

## ⚠️ This is an AI coding experiment

**Every line of code in this repository was written by an AI assistant
(Claude Sonnet / Opus) under human direction.** The goal was to see how
far a modern coding agent could push a from-scratch JavaScript engine —
lexer / parser / bytecode / VM / builtins, the lot — in a single
ongoing session.

Treat it as a research artefact, not a production runtime:

- The corpus that drives development is 549 hand-picked diff tests against
  [`modernc.org/quickjs`](https://pkg.go.dev/modernc.org/quickjs) as
  oracle; everything inside it works. Outside it, expect surprises.
- test262 conformance is 19.6% on the subset we tried — far from a
  drop-in replacement for the spec.
- No security review. Do not run untrusted scripts.

## What works

| Category | Status | Notes |
| --- | --- | --- |
| ES5 + ES2015 core | ✅ | let/const, arrow, class, template, destructuring, spread/rest, default params, for-of/for-in, labeled break/continue |
| Async | ✅ | async/await with true goroutine-based suspension on pending Promises |
| Generators | ✅ | `function*` / `yield` / `.return()` / `.next(val)` |
| Iterator protocol | ✅ | `[Symbol.iterator]`, generators, custom iterables |
| Builtins | ✅ | Math, Array (incl. ES2023 toReversed/toSorted/with), String, Object (incl. ES2024 groupBy), Number, JSON, RegExp, Date, Map/Set, WeakMap/WeakSet, Promise, Symbol primitive, BigInt, Proxy (get/set traps), Reflect, ArrayBuffer + Uint8/Int32/Float64Array, globalThis, numeric separator |
| Operators | ✅ | `**`, `??`, `?.`, `\|\|= &&= ??= **= <<= >>= >>>= &= \|= ^=` |
| Property descriptors | partial | get/set accessors work; writable/enumerable/configurable bits ignored |
| RegExp | partial | Backed by Go RE2: no lookahead / lookbehind / backreferences |
| Modules | ❌ | No `import` / `export` |
| Timers / I/O | ❌ | No `setTimeout`, no event loop beyond microtasks |
| Strict mode | ❌ | Always sloppy mode |
| Error.stack | ❌ | Throw messages but no stack frames |

## Performance

In the cross-engine benchmark suite under [`benchmark/`](./benchmark)
(arith / fib / sort / strings; see [its README](./benchmark/README.md)),
goquickjs ranks **#2 of 13** embedded scripting engines and is **the
fastest JS engine measured** — about 1.6× the upstream
`modernc.org/quickjs` (CGo binding) and 2× `goja`.

## Quick start

```sh
go get github.com/vimt/goquickjs
```

One-shot Eval — script result as a string:

```go
v, _ := goquickjs.Eval(`
  let sum = 0;
  for (let i = 1; i <= 100; i++) sum += i;
  sum
`)
fmt.Println(v) // "5050"
```

Persistent Runtime — inject Go values, call Go from JS, call JS from Go:

```go
rt := goquickjs.New()

// 1. Pass Go data into the JS scope.
_ = rt.Set("name", "world")
_ = rt.Set("nums", []int{1, 2, 3, 4})

// 2. Expose a Go function callable from JS.
rt.SetFunc("hash", func(args []goquickjs.Value) (any, error) {
    h := uint32(0)
    for _, c := range args[0].String() {
        h = h*31 + uint32(c)
    }
    return h, nil
})

// 3. Run a script that reaches into all three.
v, err := rt.Eval(`
  let total = nums.reduce((a, b) => a + b, 0);
  ` + "`" + `${name}=${total}#${hash(name)}` + "`" + `
`)
if err != nil { log.Fatal(err) }
fmt.Println(v.String()) // world=10#113318802

// 4. Pull a JS function out and call it from Go.
_, _ = rt.Eval(`function mul(a, b) { return a * b }`)
out, _ := rt.Get("mul").Call(6, 7)
fmt.Println(out.Int()) // 42

// 5. Convert any JS value back to a plain Go value.
js, _ := rt.Eval(`({user: "alice", roles: ["admin", "dev"]})`)
fmt.Println(js.ToGo()) // map[user:alice roles:[admin dev]]
```

`Set` understands all numeric types, string, bool, nil, `[]any`,
`map[string]any`, `GoFunc`, and via reflection any slice / map /
pointer of compatible element type. `Value` exposes `Int / Float /
Bool / String`, type predicates `IsNumber / IsString / IsArray / ...`,
indexed access `Get(key) / Index(i) / Len()`, and `Call` for
function-typed values. A single Runtime is **not** safe for
concurrent use — wrap with your own lock or shard one per worker.

## Architecture

```
parser  →  compiler  →  bytecode  →  vm  →  builtins
 lex.go   emit_*.go   bytecode.go   vm.go  Math/String/...
 ast.go                              helpers.go
```

- **Parser** (~2,500 LoC across 6 files): hand-written tokenizer + Pratt
  expression parser + recursive-descent statements. No external grammar
  generator.
- **Compiler**: lowers AST to a stack-machine bytecode. Closures use a
  Lua-style by-reference upvalue chain.
- **VM**: switch-dispatch interpreter with frame stack + try-handler
  table for exception unwinding. Generators and async functions each get
  their own goroutine with channel-based rendezvous for suspension.
- **Value**: 24-byte tagged struct (`tag + num + ref`). Objects use the
  V8/QuickJS shape (hidden-class) model with shared transitions.

## Testing

```sh
# Differential against modernc.org/quickjs (549 corpus, all PASS)
go test ./internal/differ/

# Race detector
go test -race ./...

# Parser + Eval fuzz (clean for 30+ seconds)
go test -fuzz=FuzzParse -fuzztime=30s ./internal/parser
go test -fuzz=FuzzEval  -fuzztime=10s .

# Goroutine-leak regression for generators + async fibers
go test -run 'Leak' .

# test262 subset (cmd-line tool, no harness deps)
TEST262=/path/to/test262 go run ./cmd/test262
```

## License

MIT — see [LICENSE](./LICENSE).
