package goquickjs

import (
	"testing"
)

// Regression suite for correctness fixes found via test262 scans. Each
// case pins a previously-observed defect (host panic, silent wrong
// answer, or wrong-mode TypeError) on the public Eval surface.

func mustEval(t *testing.T, src string) string {
	t.Helper()
	v, err := Eval(src)
	if err != nil {
		t.Fatalf("eval error: %v\nsrc: %s", err, src)
	}
	return v
}

// for-of (or for-in / switch) that throws AND catches inside its body
// must not lose the iterator/discriminant it left on the operand stack.
// Before the Handler.Depth fix, OpDup ran on an empty stack and the
// process panicked with "slice bounds out of range".
func TestUnwindPreservesForOfIterator(t *testing.T) {
	if got := mustEval(t, `
		function* g(){ yield 1; yield 2; }
		var it = g();
		var i = 0;
		for (var x of it) {
			try { throw new Error(); } catch (e) { i++; break; }
		}
		i;
	`); got != "1" {
		t.Fatalf("for-of break-from-catch: got %q want 1", got)
	}
	if got := mustEval(t, `
		function* g(){ yield 1; yield 2; }
		var it = g();
		var i = 0;
		for (var x of it) {
			try { throw new Error(); } catch (e) { i++; continue; }
		}
		i;
	`); got != "2" {
		t.Fatalf("for-of continue-from-catch: got %q want 2", got)
	}
}

// switch keeps the discriminant on the operand stack across case bodies
// the same way for-of keeps the iterator. A try/catch inside a case
// must preserve that slot.
func TestUnwindPreservesSwitchDiscriminant(t *testing.T) {
	if got := mustEval(t, `
		var hit = "";
		switch (1) {
			case 1:
				try { throw new Error(); } catch(e) { hit = "ok"; break; }
				hit = "skip";
		}
		hit;
	`); got != "ok" {
		t.Fatalf("switch catch-break: got %q want ok", got)
	}
}

// Writing a property to null/undefined must throw TypeError in every
// mode — previously we returned a Go-level ErrNotImplemented.
func TestSetPropOnNullishThrows(t *testing.T) {
	// Inspect the thrown value's constructor name JS-side — host
	// err.Error() only serializes the .message field.
	for _, src := range []string{
		`try { null.x = 1; "miss" } catch (e) { e.name }`,
		`try { undefined.x = 1; "miss" } catch (e) { e.name }`,
		`try { null[1] = 1; "miss" } catch (e) { e.name }`,
	} {
		if got := mustEval(t, src); got != "TypeError" {
			t.Fatalf("%s\n  got %q want TypeError", src, got)
		}
	}
}

// Sloppy-mode write to a primitive wrapper is a silent no-op — it must
// NOT throw, but the assignment is observably discarded.
func TestSetPropOnPrimitiveIsSilent(t *testing.T) {
	if got := mustEval(t, `
		var x = 5;
		x.foo = 1;            // wrapper-and-discard; no throw, no effect
		typeof x + ":" + x.foo;
	`); got != "number:undefined" {
		t.Fatalf("primitive prop write: got %q want number:undefined", got)
	}
}

// Oversized ArrayBuffer / TypedArray lengths must surface as JS-level
// RangeError, not a host panic — embedded libraries are not allowed
// to crash the host process.
// Calling or new-ing a non-function must throw a CATCHABLE JS
// TypeError, not a Go-side ErrNotImplemented. test262 relies on this
// — assert.throws(TypeError, () => undefined()) is everywhere.
func TestCallingNonFunctionThrowsCatchableTypeError(t *testing.T) {
	cases := []string{
		`try { undefined(); "miss" } catch (e) { e.name }`,
		`try { (1).foo(); "miss" } catch (e) { e.name }`,            // method call
		`try { new undefined(); "miss" } catch (e) { e.name }`,      // new
		`try { Function.prototype.call.call(1); "miss" } catch (e) { e.name }`,
	}
	for _, src := range cases {
		if got := mustEval(t, src); got != "TypeError" {
			t.Fatalf("%s\n  got %q want TypeError", src, got)
		}
	}
}

// Foo.prototype must be reachable on each major builtin so test262
// patterns like `String.prototype.charAt.call(thisArg)` work.
func TestBuiltinPrototypesExposed(t *testing.T) {
	cases := map[string]string{
		`typeof Array.prototype.push`:       "function",
		`typeof String.prototype.charAt`:    "function",
		`typeof Number.prototype.toString`:  "function",
		`typeof Function.prototype.call`:    "function",
		`typeof Object.prototype.hasOwnProperty`: "function",
		`typeof Boolean.prototype`:          "object",
	}
	for src, want := range cases {
		if got := mustEval(t, src); got != want {
			t.Fatalf("%s\n  got %q want %q", src, got, want)
		}
	}
}

// Compound assignment must now work on member / index targets too:
//   o.x += 1, a[i] *= 2, o.x ||= "default", etc.
// Previously the parser refused these outright.
func TestCompoundAssignMemberAndIndex(t *testing.T) {
	cases := map[string]string{
		`var o={x:10}; o.x+=5; o.x`:                "15",
		`var o={x:10}; o.x*=2; o.x`:                "20",
		`var o={x:0};  o.x||=7; o.x`:               "7",
		`var o={x:9};  o.x||=99; o.x`:              "9",
		`var o={x:1};  o.x&&=2; o.x`:               "2",
		`var o={x:null}; o.x??=3; o.x`:             "3",
		`var a=[1,2,3]; a[1]-=10; a[1]`:            "-8",
		`var a=[1,2,3]; a[0]=a[2]+=5; a.join(",")`: "8,2,8",
	}
	for src, want := range cases {
		if got := mustEval(t, src); got != want {
			t.Fatalf("%s\n  got %q want %q", src, got, want)
		}
	}
}

// Identifier names may contain Unicode escapes (\uXXXX or \u{HEX});
// the test262 corpus generates thousands of these by template. We
// only need the escape mechanism — full Unicode ID_Start tables are
// not in scope (we accept any rune >= 0x80 as an ident part).
func TestIdentifierUnicodeEscapes(t *testing.T) {
	cases := map[string]string{
		// f = 'f', o = 'o' — spells "foo"
		"var foo = 7; foo":              "7",
		"var f\\u006Fo = 8; foo":             "8",
		// \u{XXXXXX} braced form
		"var \\u{66}oo = 9; foo":             "9",
		// Escape inside an object method name
		`var o = { value() { return 1 } }; o.value()`: "1",
	}
	for src, want := range cases {
		if got := mustEval(t, src); got != want {
			t.Fatalf("%s\n  got %q want %q", src, got, want)
		}
	}
}

// String/Number/Boolean must be callable as ToPrimitive coercion
// functions: `String(1) === "1"`, `Number("3") === 3`, `Boolean(0) === false`.
func TestPrimitiveCtorsCoerce(t *testing.T) {
	cases := map[string]string{
		`typeof String`:   "function",
		`typeof Number`:   "function",
		`typeof Boolean`:  "function",
		`String(1)`:       "1",
		`String(null)`:    "null",
		`String()`:        "",
		`Number("3")`:     "3",
		`Number(true)`:    "1",
		`Number()`:        "0",
		`Boolean(0)`:      "false",
		`Boolean("x")`:    "true",
		`Boolean()`:       "false",
		`Boolean(null)`:   "false",
	}
	for src, want := range cases {
		if got := mustEval(t, src); got != want {
			t.Fatalf("%s\n  got %q want %q", src, got, want)
		}
	}
}

// Destructuring assignment at expression position: parser now coerces
// `{...}` / `[...]` on the LHS of `=` into a Pattern, and the compiler
// emits resolve+store (not declare+store).
func TestDestructuringAssignment(t *testing.T) {
	cases := map[string]string{
		// Object: simple, renamed, nested, with rest.
		`var a,b; ({a, b} = {a:1, b:2}); a + ":" + b`:                   "1:2",
		`var x,y; ({a: x, b: y} = {a:7, b:8}); x + ":" + y`:             "7:8",
		`var p,q,r; ({a:{p, q}, r} = {a:{p:1,q:2}, r:3}); p+":"+q+":"+r`: "1:2:3",
		`var a, rest; ({a, ...rest} = {a:1, b:2, c:3}); a + ":" + rest.b + ":" + rest.c`: "1:2:3",
		// Array: simple, hole, rest.
		`var x,y,z; [x,y,z] = [1,2,3]; x+":"+y+":"+z`:           "1:2:3",
		`var x, r; [x, ...r] = [1,2,3,4]; x + ":" + r.join(",")`: "1:2,3,4",
		// Completion value of an assignment expression is the rhs.
		`var a,b; var v = ({a, b} = {a:5, b:6}); v.a + ":" + v.b`: "5:6",
		// Chained assignment uses the completion value.
		`var a,b,c; c = ({a, b} = {a:1, b:2}); a + ":" + b + ":" + (c.a+c.b)`: "1:2:3",
		// Defaults: object-shorthand and array.
		`var a,b; ({a = 1, b = 2} = {}); a + ":" + b`:                "1:2",
		`var a,b; ({a = 1, b = 2} = {b: 99}); a + ":" + b`:           "1:99",
		`var x,y; [x = 5, y = 6] = []; x + ":" + y`:                   "5:6",
		`var x,y; [x = 5, y = 6] = [10]; x + ":" + y`:                 "10:6",
	}
	for src, want := range cases {
		if got := mustEval(t, src); got != want {
			t.Fatalf("%s\n  got %q want %q", src, got, want)
		}
	}
}

func TestOversizedArrayBufferRangeError(t *testing.T) {
	for _, src := range []string{
		`try { new ArrayBuffer(2 ** 53); "miss" } catch (e) { e.name }`,
		`try { new Int32Array(2 ** 40); "miss" } catch (e) { e.name }`,
	} {
		if got := mustEval(t, src); got != "RangeError" {
			t.Fatalf("%s\n  got %q want RangeError", src, got)
		}
	}
}
