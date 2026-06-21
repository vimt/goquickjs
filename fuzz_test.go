package goquickjs

import (
	"testing"
	"time"
)

// FuzzEval drives the full pipeline (parser → compiler → vm). We
// allow any returned error but treat panic, hang, or runaway alloc
// as a bug. A per-invocation timeout caps pathological inputs.
func FuzzEval(f *testing.F) {
	seeds := []string{
		"1+2",
		"let x=5; x*2",
		"function f(n){return n<2?n:f(n-1)+f(n-2)} f(10)",
		"[1,2,3].map(x=>x*x).reduce((a,b)=>a+b,0)",
		"class A{m(){return 42}} new A().m()",
		"try{throw 'x'}catch(e){e}",
		"`hello ${1+2}`",
		"async function f(){return await Promise.resolve(1)} f()",
		"function* g(){yield 1;yield 2} [...g()]",
		"({a:1, b:2})['a']",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 2048 {
			t.Skip()
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = Eval(src)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("eval hung: %q", src)
		}
	})
}
