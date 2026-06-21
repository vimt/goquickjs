package goquickjs

import (
	"fmt"
	"reflect"

	"github.com/vimt/goquickjs/internal/builtins"
	"github.com/vimt/goquickjs/internal/compiler"
	"github.com/vimt/goquickjs/internal/parser"
	"github.com/vimt/goquickjs/internal/value"
	"github.com/vimt/goquickjs/internal/vm"
)

// Runtime is a JS execution context with persistent globals. Use it
// when you need to keep state across multiple Eval calls or hand Go
// data / Go functions to JS code.
//
// Each Runtime is independent: built-ins are installed once, mutations
// the script makes to e.g. Array.prototype don't leak to other
// Runtimes. The zero value is not usable — call New.
//
// A Runtime is NOT safe for concurrent use from multiple goroutines.
type Runtime struct {
	globals map[string]value.Value
	vm      *vm.VM
}

// New creates a fresh Runtime with all standard JS built-ins
// installed.
func New() *Runtime {
	g := map[string]value.Value{}
	builtins.Install(g)
	return &Runtime{globals: g, vm: vm.NewVM(g)}
}

// Eval parses, compiles, and runs JS source against the Runtime's
// shared globals. The completion value is returned as a Value
// wrapper; on parser/compile/runtime error err is non-nil.
func (r *Runtime) Eval(src string) (Value, error) {
	prog, err := parser.Parse(src)
	if err != nil {
		return Value{}, err
	}
	chunk, err := compiler.Compile(prog)
	if err != nil {
		return Value{}, err
	}
	v, err := r.vm.RunChunk(chunk)
	if err != nil {
		return Value{}, err
	}
	return Value{v: v, rt: r}, nil
}

// Set installs a Go value as a JS global named name. Recognised Go
// types — string, bool, all numeric primitives, nil, []interface{},
// map[string]interface{}, GoFunc, and any *Value previously returned
// from this Runtime — are converted to the matching JS value.
// Anything else returns an error so callers don't silently get
// unexpected behaviour.
func (r *Runtime) Set(name string, x interface{}) error {
	v, err := toJS(x)
	if err != nil {
		return fmt.Errorf("Set(%q): %w", name, err)
	}
	r.globals[name] = v
	return nil
}

// SetFunc installs a Go callable as a JS global function named name.
// The callback receives the JS-side arguments as Value wrappers; its
// return value is converted back via the same rules as Set.
func (r *Runtime) SetFunc(name string, fn GoFunc) {
	native := &value.Function{
		Name:  name,
		Arity: 0,
		Native: func(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
			wrapped := make([]Value, len(args))
			for i, a := range args {
				wrapped[i] = Value{v: a, rt: r}
			}
			out, err := fn(wrapped)
			if err != nil {
				return value.Value{}, err
			}
			return toJS(out)
		},
	}
	r.globals[name] = value.FunctionVal(native)
}

// Get returns the current value of the JS global named name. If
// undefined, the returned Value's IsUndefined() reports true.
func (r *Runtime) Get(name string) Value {
	v, ok := r.globals[name]
	if !ok {
		return Value{v: value.Undefined(), rt: r}
	}
	return Value{v: v, rt: r}
}

// GoFunc is the Go-side signature for functions exposed to JS via
// SetFunc. Return any Go value Set understands (or a Value); err
// surfaces in JS as a thrown error.
type GoFunc func(args []Value) (interface{}, error)

// Value is a wrapper around a JS value with typed accessors. Values
// are pinned to the Runtime that produced them — Call routes back
// through that Runtime's VM.
type Value struct {
	v  value.Value
	rt *Runtime
}

// String returns the value's canonical string rendering, matching
// what `String(x)` produces in JS.
func (v Value) String() string { return v.v.String() }

// Int returns the value's numeric form truncated to int64. Non-
// numbers coerce via the engine's AsNumber (boolean → 0/1, others →
// implementation-defined).
func (v Value) Int() int64 { return int64(v.v.AsNumber()) }

// Float returns the value's underlying float64. For non-numbers
// behaviour matches AsNumber (0 for objects/strings — call ToNumber
// equivalent in JS if you need coercion).
func (v Value) Float() float64 { return v.v.AsNumber() }

// Bool returns the value's boolean payload. For non-booleans it
// follows the truthiness convention via AsBool on numbers.
func (v Value) Bool() bool {
	switch v.v.Type() {
	case value.TypeBool:
		return v.v.AsBool()
	case value.TypeNumber:
		f := v.v.AsNumber()
		return f != 0 && f == f
	case value.TypeString:
		return v.v.AsString() != ""
	case value.TypeUndefined, value.TypeNull:
		return false
	}
	return true
}

// IsUndefined reports whether the value is exactly `undefined`.
func (v Value) IsUndefined() bool { return v.v.Type() == value.TypeUndefined }

// IsNull reports whether the value is exactly `null`.
func (v Value) IsNull() bool { return v.v.Type() == value.TypeNull }

// IsNumber reports whether the value is a JS number.
func (v Value) IsNumber() bool { return v.v.Type() == value.TypeNumber }

// IsString reports whether the value is a JS string.
func (v Value) IsString() bool { return v.v.Type() == value.TypeString }

// IsObject reports whether the value is a plain JS object.
func (v Value) IsObject() bool { return v.v.Type() == value.TypeObject }

// IsArray reports whether the value is a JS array.
func (v Value) IsArray() bool { return v.v.Type() == value.TypeArray }

// IsFunction reports whether the value is callable.
func (v Value) IsFunction() bool { return v.v.Type() == value.TypeFunction }

// Get reads a property from an object/array value. For arrays,
// numeric-looking keys (e.g. "0") take the indexed path. Returns
// undefined when the key is absent or the receiver isn't object-like.
func (v Value) Get(key string) Value {
	switch v.v.Type() {
	case value.TypeObject:
		out, _ := v.v.AsObject().Get(key)
		return Value{v: out, rt: v.rt}
	case value.TypeArray:
		return Value{v: v.v.AsArray().Prop(key), rt: v.rt}
	}
	return Value{v: value.Undefined(), rt: v.rt}
}

// Len returns the number of elements for arrays / number of own
// keys for objects / character count for strings. -1 for other
// kinds (number / bool / undefined / null / function).
func (v Value) Len() int {
	switch v.v.Type() {
	case value.TypeArray:
		return v.v.AsArray().Length()
	case value.TypeString:
		return len(v.v.AsString())
	case value.TypeObject:
		return len(v.v.AsObject().PropNames())
	}
	return -1
}

// Index reads index i from an array value. Out-of-range or non-array
// receivers return undefined.
func (v Value) Index(i int) Value {
	if v.v.Type() == value.TypeArray {
		return Value{v: v.v.AsArray().Get(i), rt: v.rt}
	}
	return Value{v: value.Undefined(), rt: v.rt}
}

// Call invokes a function-typed Value with the supplied Go args.
// args are converted via the same rules as Set. Returns the JS-side
// completion value or any thrown error.
func (v Value) Call(args ...interface{}) (Value, error) {
	if v.v.Type() != value.TypeFunction {
		return Value{}, fmt.Errorf("Call: receiver is not a function (got %v)", v.v.Type())
	}
	if v.rt == nil {
		return Value{}, fmt.Errorf("Call: value not bound to a Runtime")
	}
	jsArgs := make([]value.Value, len(args))
	for i, a := range args {
		out, err := toJS(a)
		if err != nil {
			return Value{}, fmt.Errorf("Call: arg %d: %w", i, err)
		}
		jsArgs[i] = out
	}
	ret, err := v.rt.vm.Call(v.v.AsFunction(), value.Undefined(), jsArgs)
	if err != nil {
		return Value{}, err
	}
	return Value{v: ret, rt: v.rt}, nil
}

// ToGo converts the JS value back into the closest plain Go type:
// string / float64 / bool / nil / []interface{} / map[string]interface{}.
// Functions are returned as a callable closure that wraps Call.
// Useful when the host wants to consume results without the Value
// wrapper.
func (v Value) ToGo() interface{} {
	switch v.v.Type() {
	case value.TypeUndefined, value.TypeNull:
		return nil
	case value.TypeBool:
		return v.v.AsBool()
	case value.TypeNumber:
		return v.v.AsNumber()
	case value.TypeString:
		return v.v.AsString()
	case value.TypeArray:
		arr := v.v.AsArray()
		out := make([]interface{}, arr.Length())
		for i := 0; i < arr.Length(); i++ {
			out[i] = (Value{v: arr.Get(i), rt: v.rt}).ToGo()
		}
		return out
	case value.TypeObject:
		o := v.v.AsObject()
		out := map[string]interface{}{}
		for _, k := range o.PropNames() {
			val, _ := o.GetOwn(k)
			out[k] = (Value{v: val, rt: v.rt}).ToGo()
		}
		return out
	case value.TypeFunction:
		rt := v.rt
		fn := v.v.AsFunction()
		return func(args ...interface{}) (interface{}, error) {
			jsArgs := make([]value.Value, len(args))
			for i, a := range args {
				out, err := toJS(a)
				if err != nil {
					return nil, err
				}
				jsArgs[i] = out
			}
			ret, err := rt.vm.Call(fn, value.Undefined(), jsArgs)
			if err != nil {
				return nil, err
			}
			return (Value{v: ret, rt: rt}).ToGo(), nil
		}
	}
	return nil
}

// toJS is the Go → JS conversion entry point used by Set, SetFunc
// return paths, and Call's arg path. The mapping is deliberately
// narrow: anything outside the listed types errors out so callers
// don't see surprising fallbacks.
func toJS(x interface{}) (value.Value, error) {
	if x == nil {
		return value.Null(), nil
	}
	switch v := x.(type) {
	case Value:
		return v.v, nil
	case bool:
		return value.Bool(v), nil
	case int:
		return value.Number(float64(v)), nil
	case int8:
		return value.Number(float64(v)), nil
	case int16:
		return value.Number(float64(v)), nil
	case int32:
		return value.Number(float64(v)), nil
	case int64:
		return value.Number(float64(v)), nil
	case uint:
		return value.Number(float64(v)), nil
	case uint8:
		return value.Number(float64(v)), nil
	case uint16:
		return value.Number(float64(v)), nil
	case uint32:
		return value.Number(float64(v)), nil
	case uint64:
		return value.Number(float64(v)), nil
	case float32:
		return value.Number(float64(v)), nil
	case float64:
		return value.Number(v), nil
	case string:
		return value.String(v), nil
	case []interface{}:
		arr := value.NewArrayWithCap(len(v))
		for _, it := range v {
			conv, err := toJS(it)
			if err != nil {
				return value.Value{}, err
			}
			arr.Push(conv)
		}
		return value.ArrayVal(arr), nil
	case map[string]interface{}:
		o := value.NewObject()
		for k, it := range v {
			conv, err := toJS(it)
			if err != nil {
				return value.Value{}, err
			}
			o.Set(k, conv)
		}
		return value.ObjectVal(o), nil
	case GoFunc:
		fn := &value.Function{
			Name: "anonymous", Arity: 0,
			Native: func(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
				wrapped := make([]Value, len(args))
				for i, a := range args {
					wrapped[i] = Value{v: a}
				}
				out, err := v(wrapped)
				if err != nil {
					return value.Value{}, err
				}
				return toJS(out)
			},
		}
		return value.FunctionVal(fn), nil
	}
	// Reflection fallback: slices / maps with concrete element types
	// (e.g. []int, map[string]string) are common host data shapes.
	return toJSReflect(x)
}

func toJSReflect(x interface{}) (value.Value, error) {
	rv := reflect.ValueOf(x)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		arr := value.NewArrayWithCap(rv.Len())
		for i := 0; i < rv.Len(); i++ {
			conv, err := toJS(rv.Index(i).Interface())
			if err != nil {
				return value.Value{}, err
			}
			arr.Push(conv)
		}
		return value.ArrayVal(arr), nil
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return value.Value{}, fmt.Errorf("toJS: map key must be string, got %v", rv.Type().Key())
		}
		o := value.NewObject()
		iter := rv.MapRange()
		for iter.Next() {
			conv, err := toJS(iter.Value().Interface())
			if err != nil {
				return value.Value{}, err
			}
			o.Set(iter.Key().String(), conv)
		}
		return value.ObjectVal(o), nil
	case reflect.Ptr:
		if rv.IsNil() {
			return value.Null(), nil
		}
		return toJS(rv.Elem().Interface())
	}
	return value.Value{}, fmt.Errorf("toJS: unsupported Go type %T", x)
}
