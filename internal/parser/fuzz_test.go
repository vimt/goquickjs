package parser

import "testing"

// FuzzParse feeds random byte sequences to the parser to verify it
// only returns errors — never panics or hangs. Seed corpus pulls a
// few representative source snippets so the fuzzer has shape to
// mutate from.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"1+2",
		"let x = 5;",
		"function f(){return 1}",
		"class A{m(){}}",
		"`tpl${x}end`",
		"async function f(){await p}",
		"/regex/i",
		"1n + 2n",
		"for (let k in o) {}",
		"let {a,b=3,...r} = o;",
		"(...a) => a",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		// Cap source length so a pathological case doesn't OOM the
		// fuzzer host; we're hunting panics, not perf bugs.
		if len(src) > 4096 {
			t.Skip()
		}
		// Parser may return error — that's fine. Panic is the bug.
		_, _ = Parse(src)
	})
}
