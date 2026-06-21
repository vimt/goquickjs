package goquickjs

import (
	"runtime"
	"testing"
	"time"
)

// TestGeneratorReturnNoLeak creates a many short-lived generators
// that the user `.return()`s mid-iteration and asserts the number
// of live goroutines doesn't climb in proportion. Pre-fix this
// would leak one goroutine per `.return()`.
func TestGeneratorReturnNoLeak(t *testing.T) {
	src := `
		function* g() {
			while (true) yield 1;
		}
		for (let i = 0; i < 1000; i++) {
			let it = g();
			it.next();
			it.return();
		}
		"done"
	`
	before := runtime.NumGoroutine()
	v, err := Eval(src)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != "done" {
		t.Fatalf("got %q want done", v)
	}
	// Give the killed goroutines a moment to exit and the runtime
	// a chance to GC their stacks before we sample.
	for i := 0; i < 10; i++ {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
		if runtime.NumGoroutine() <= before+5 {
			break
		}
	}
	after := runtime.NumGoroutine()
	if after > before+10 {
		t.Fatalf("goroutine leak: before=%d after=%d (delta=%d)", before, after, after-before)
	}
	t.Logf("goroutines: before=%d after=%d", before, after)
}

// TestAsyncFiberNoLeak similarly checks that async functions that
// finish (whether resolved or rejected) clean up their fiber.
func TestAsyncFiberNoLeak(t *testing.T) {
	src := `
		async function f() {
			let p = new Promise(r => Promise.resolve().then(() => r("x")));
			return await p;
		}
		let results = [];
		for (let i = 0; i < 500; i++) {
			f().then(v => results.push(v));
		}
		results.length
	`
	before := runtime.NumGoroutine()
	if _, err := Eval(src); err != nil {
		t.Fatalf("eval: %v", err)
	}
	for i := 0; i < 10; i++ {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
		if runtime.NumGoroutine() <= before+5 {
			break
		}
	}
	after := runtime.NumGoroutine()
	if after > before+10 {
		t.Fatalf("goroutine leak: before=%d after=%d (delta=%d)", before, after, after-before)
	}
	t.Logf("goroutines: before=%d after=%d", before, after)
}
