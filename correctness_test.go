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
