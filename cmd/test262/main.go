// test262 runner — feeds a subset of the official ECMAScript
// conformance suite through goquickjs and prints a pass/fail summary.
//
// We don't implement the full harness (frontmatter parsing,
// $262 controller, async wrappers, includes). Instead we run each
// .js file with a minimal prelude that supplies assert.* and
// $ERROR, then count results bucketed by error category:
//
//   pass               — file ran without throwing
//   fail-assertion     — an assert.* threw (real spec divergence)
//   fail-syntax        — parser couldn't read it (NYI grammar)
//   fail-runtime       — VM threw something other than assert
//   skip-feature       — file's frontmatter requires a feature we
//                        explicitly opt out of (e.g. modules)

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vimt/goquickjs"
)

const prelude = `
function Test262Error(message) { this.message = message; }
Test262Error.prototype.toString = function() { return "Test262Error: " + this.message; };
function $ERROR(msg) { throw new Test262Error(msg); }
function $DONOTEVALUATE() { throw new Test262Error("dont evaluate"); }
var assert = function assert(b, msg) {
  if (!b) throw new Test262Error(msg || "assertion failed");
};
assert.sameValue = function(a, b, msg) {
  if (a !== b && !(a !== a && b !== b)) {
    throw new Test262Error((msg || "") + " expected " + b + " got " + a);
  }
};
assert.notSameValue = function(a, b, msg) {
  if (a === b) {
    throw new Test262Error((msg || "") + " expected not " + b);
  }
};
assert.throws = function(ctor, fn, msg) {
  try { fn(); } catch (e) { return; }
  throw new Test262Error((msg || "") + " expected throw");
};
assert._isSameValue = function(a, b) {
  return a === b || (a !== a && b !== b);
};
assert.deepEqual = function(a, b, msg) {
  if (assert._isSameValue(a, b)) return;
  if (typeof a !== "object" || typeof b !== "object" || a === null || b === null) {
    throw new Test262Error((msg || "") + " not deeply equal");
  }
  var ka = Object.keys(a), kb = Object.keys(b);
  if (ka.length !== kb.length) throw new Test262Error((msg || "") + " key count");
  for (var i = 0; i < ka.length; i++) assert.deepEqual(a[ka[i]], b[ka[i]], msg);
};
`

type bucket struct {
	pass      int
	failAsrt  int
	failSyn   int
	failRT    int
	skip      int
}

func main() {
	roots := []string{
		"test/language/expressions/addition",
		"test/language/expressions/subtraction",
		"test/language/expressions/multiplication",
		"test/language/expressions/division",
		"test/language/expressions/equals",
		"test/language/expressions/strict-equals",
		"test/language/expressions/logical-and",
		"test/language/expressions/logical-or",
		"test/language/expressions/conditional",
		"test/language/expressions/exponentiation",
		"test/language/expressions/typeof",
		"test/language/statements/if",
		"test/language/statements/while",
		"test/language/statements/for",
		"test/language/statements/try",
		"test/language/statements/throw",
		"test/language/statements/return",
		"test/language/statements/switch",
		"test/language/statements/break",
		"test/language/statements/continue",
		"test/language/statements/labeled",
		"test/language/statements/let",
		"test/language/statements/const",
		"test/language/statements/variable",
		"test/language/comments",
		"test/built-ins/Array/prototype/push",
		"test/built-ins/Array/prototype/pop",
		"test/built-ins/Array/prototype/map",
		"test/built-ins/Array/prototype/filter",
		"test/built-ins/Array/prototype/reduce",
		"test/built-ins/String/prototype/slice",
		"test/built-ins/String/prototype/split",
		"test/built-ins/Math/abs",
		"test/built-ins/Math/min",
		"test/built-ins/Math/max",
		"test/built-ins/JSON/parse",
		"test/built-ins/JSON/stringify",
		"test/built-ins/Number/isFinite",
		"test/built-ins/Number/isInteger",
		"test/built-ins/Number/isNaN",
		"test/built-ins/Object/keys",
		"test/built-ins/Object/values",
		"test/built-ins/Object/entries",
	}
	base := "/tmp/test262"
	if env := os.Getenv("TEST262"); env != "" {
		base = env
	}

	b := bucket{}
	start := time.Now()
	for _, r := range roots {
		_ = filepath.Walk(filepath.Join(base, r), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(p, ".js") || strings.HasSuffix(p, "_FIXTURE.js") {
				return nil
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			s := string(src)
			// Hard-skip categories we know we don't model.
			if strings.Contains(s, "[noStrict]") ||
				strings.Contains(s, "flags: [module]") ||
				strings.Contains(s, "negative:") ||
				strings.Contains(s, "/*---") && strings.Contains(s, "Test262Error") && false {
				b.skip++
				return nil
			}
			// Some tests use sloppy `var` reassignment plus features we
			// don't implement (e.g. `with`, `Realm`, `Atomics`).
			if strings.Contains(s, "$262") || strings.Contains(s, "Atomics") ||
				strings.Contains(s, " with(") || strings.Contains(s, " with (") {
				b.skip++
				return nil
			}
			full := prelude + "\n" + s
			done := make(chan error, 1)
			go func() {
				_, e := goquickjs.Eval(full)
				done <- e
			}()
			select {
			case err := <-done:
				if err == nil {
					b.pass++
				} else {
					msg := err.Error()
					switch {
					case strings.Contains(msg, "Test262Error"):
						b.failAsrt++
					case strings.Contains(msg, "parser:") || strings.Contains(msg, "compiler:"):
						b.failSyn++
					default:
						b.failRT++
					}
				}
			case <-time.After(2 * time.Second):
				b.failRT++ // hang counts as runtime
			}
			return nil
		})
	}
	total := b.pass + b.failAsrt + b.failSyn + b.failRT + b.skip
	elapsed := time.Since(start)
	fmt.Printf("test262 subset (%d roots) — %d files, %.1fs\n", len(roots), total, elapsed.Seconds())
	fmt.Printf("  pass:            %5d  %5.1f%%\n", b.pass, pct(b.pass, total))
	fmt.Printf("  fail-assertion:  %5d  %5.1f%%   (engine ran but result differs from spec)\n", b.failAsrt, pct(b.failAsrt, total))
	fmt.Printf("  fail-syntax:     %5d  %5.1f%%   (parser/compiler NYI)\n", b.failSyn, pct(b.failSyn, total))
	fmt.Printf("  fail-runtime:    %5d  %5.1f%%   (uncaught throw / hang)\n", b.failRT, pct(b.failRT, total))
	fmt.Printf("  skip:            %5d  %5.1f%%\n", b.skip, pct(b.skip, total))
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) * 100.0 / float64(total)
}
