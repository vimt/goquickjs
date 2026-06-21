package goquickjs

import (
	"errors"
	"testing"
)

func TestRuntimeEvalReturnsValue(t *testing.T) {
	r := New()
	v, err := r.Eval(`1 + 2`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Int() != 3 {
		t.Fatalf("want 3 got %d", v.Int())
	}
	if !v.IsNumber() {
		t.Fatalf("want number type")
	}
}

func TestRuntimeSetGoScalarsVisibleToJS(t *testing.T) {
	r := New()
	if err := r.Set("name", "world"); err != nil {
		t.Fatal(err)
	}
	if err := r.Set("year", 2026); err != nil {
		t.Fatal(err)
	}
	if err := r.Set("ok", true); err != nil {
		t.Fatal(err)
	}
	v, err := r.Eval(`name + ":" + year + ":" + ok`)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.String(); got != "world:2026:true" {
		t.Fatalf("got %q", got)
	}
}

func TestRuntimeSetSliceAndMap(t *testing.T) {
	r := New()
	if err := r.Set("nums", []int{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := r.Set("meta", map[string]any{"a": 1, "b": "x"}); err != nil {
		t.Fatal(err)
	}
	v, err := r.Eval(`nums.reduce((s, x) => s + x, 0) + ":" + meta.a + ":" + meta.b`)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.String(); got != "6:1:x" {
		t.Fatalf("got %q", got)
	}
}

func TestRuntimeSetFuncCallableFromJS(t *testing.T) {
	r := New()
	r.SetFunc("addAll", func(args []Value) (any, error) {
		sum := 0.0
		for _, a := range args {
			sum += a.Float()
		}
		return sum, nil
	})
	v, err := r.Eval(`addAll(1, 2, 3, 4)`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Float() != 10 {
		t.Fatalf("got %v", v.Float())
	}
}

func TestRuntimeSetFuncErrorPropagates(t *testing.T) {
	r := New()
	r.SetFunc("boom", func(args []Value) (any, error) {
		return nil, errors.New("kaboom")
	})
	_, err := r.Eval(`boom()`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRuntimeCallJSFromGo(t *testing.T) {
	r := New()
	if _, err := r.Eval(`function mul(a, b) { return a * b; }`); err != nil {
		t.Fatal(err)
	}
	fn := r.Get("mul")
	if !fn.IsFunction() {
		t.Fatal("mul is not a function")
	}
	ret, err := fn.Call(6, 7)
	if err != nil {
		t.Fatal(err)
	}
	if ret.Int() != 42 {
		t.Fatalf("got %d", ret.Int())
	}
}

func TestRuntimeRoundTripObject(t *testing.T) {
	r := New()
	v, err := r.Eval(`({a: 1, b: [2, 3], c: "hi"})`)
	if err != nil {
		t.Fatal(err)
	}
	g := v.ToGo()
	m, ok := g.(map[string]any)
	if !ok {
		t.Fatalf("ToGo not map: %T", g)
	}
	if m["a"].(float64) != 1 {
		t.Fatalf("a=%v", m["a"])
	}
	arr := m["b"].([]any)
	if len(arr) != 2 || arr[0].(float64) != 2 || arr[1].(float64) != 3 {
		t.Fatalf("b=%v", arr)
	}
	if m["c"].(string) != "hi" {
		t.Fatalf("c=%v", m["c"])
	}
}

func TestRuntimeGetUndefinedGlobal(t *testing.T) {
	r := New()
	v := r.Get("notDefinedAnywhere")
	if !v.IsUndefined() {
		t.Fatalf("got %v", v.String())
	}
}

func TestRuntimeStatePersistsAcrossEvals(t *testing.T) {
	r := New()
	if _, err := r.Eval(`let counter = 0; function bump() { counter++; return counter; }`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		v, err := r.Eval(`bump()`)
		if err != nil {
			t.Fatal(err)
		}
		if v.Int() != int64(i) {
			t.Fatalf("iter %d: got %d", i, v.Int())
		}
	}
}

func TestRuntimeGoFuncReturningGoMap(t *testing.T) {
	r := New()
	r.SetFunc("makeUser", func(args []Value) (any, error) {
		return map[string]any{
			"id":   args[0].Int(),
			"name": args[1].String(),
		}, nil
	})
	v, err := r.Eval(`let u = makeUser(7, "alice"); u.id + ":" + u.name`)
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "7:alice" {
		t.Fatalf("got %q", v.String())
	}
}
