// Array constructor object and Array.prototype methods.
//
// Prototype methods are registered into value.ArrayProto (see
// registerArrayPrototype). Static methods (Array.isArray, ...) go
// on the constructor object built by installArray.

package builtins

import (
	"math"
	"sort"
	"strings"

	"github.com/vimt/goquickjs/internal/value"
)

func installArray(globals map[string]value.Value) {
	// Array constructor: a namespace object today (we do not yet
	// support `new Array(...)` or `Array(...)` as a call). Static
	// methods get hung off it.
	ctor := value.NewObject()
	ctor.Set("isArray", nativeFn("isArray", 1, arrayIsArray))
	ctor.Set("from", nativeFn("from", 1, arrayFrom))
	ctor.Set("of", nativeFn("of", 0, arrayOf))
	globals["Array"] = value.ObjectVal(ctor)
}

func arrayIsArray(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Bool(argOrUndef(args, 0).Type() == value.TypeArray), nil
}

// arrayFrom implements Array.from(iterable, mapFn?, thisArg?).
// Supported iterable shapes: Array, String, and "array-like" objects
// (a TypeObject with a .length property). Full Symbol.iterator
// protocol is out of scope.
func arrayFrom(caller value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	src := argOrUndef(args, 0)
	var mapFn *value.Function
	if mf := argOrUndef(args, 1); mf.Type() == value.TypeFunction {
		mapFn = mf.AsFunction()
	}
	thisArg := argOrUndef(args, 2)

	out := value.NewArray()
	push := func(elem value.Value, i int) error {
		if mapFn != nil {
			mapped, err := caller.Call(mapFn, thisArg, []value.Value{elem, value.Number(float64(i))})
			if err != nil {
				return err
			}
			out.Push(mapped)
			return nil
		}
		out.Push(elem)
		return nil
	}

	switch src.Type() {
	case value.TypeArray:
		arr := src.AsArray()
		for i := 0; i < arr.Length(); i++ {
			if err := push(arr.Get(i), i); err != nil {
				return value.Value{}, err
			}
		}
	case value.TypeString:
		s := src.AsString()
		for i := 0; i < len(s); i++ {
			if err := push(value.String(string(s[i])), i); err != nil {
				return value.Value{}, err
			}
		}
	case value.TypeObject:
		// array-like: must have a length.
		o := src.AsObject()
		lenV, _ := o.Get("length")
		if lenV.Type() != value.TypeNumber {
			return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Array.from: value is not iterable")}
		}
		n := int(lenV.AsNumber())
		if n < 0 {
			n = 0
		}
		for i := 0; i < n; i++ {
			// keys are stringified integers.
			elem, _ := o.Get(intToKey(i))
			if err := push(elem, i); err != nil {
				return value.Value{}, err
			}
		}
	default:
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Array.from: value is not iterable")}
	}
	return value.ArrayVal(out), nil
}

// arrayOf returns a fresh Array containing all positional args.
func arrayOf(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	out := value.NewArray()
	for _, a := range args {
		out.Push(a)
	}
	return value.ArrayVal(out), nil
}

// intToKey renders i as a decimal string for use as an object key,
// matching how JS coerces integer property accesses on plain objects.
func intToKey(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func registerArrayPrototype() {
	value.ArrayProto["push"] = &value.Function{Name: "push", Arity: 1, Native: arrayPush}
	value.ArrayProto["pop"] = &value.Function{Name: "pop", Arity: 0, Native: arrayPop}
	value.ArrayProto["shift"] = &value.Function{Name: "shift", Arity: 0, Native: arrayShift}
	value.ArrayProto["unshift"] = &value.Function{Name: "unshift", Arity: 1, Native: arrayUnshift}
	value.ArrayProto["reverse"] = &value.Function{Name: "reverse", Arity: 0, Native: arrayReverse}
	value.ArrayProto["fill"] = &value.Function{Name: "fill", Arity: 1, Native: arrayFill}
	value.ArrayProto["slice"] = &value.Function{Name: "slice", Arity: 2, Native: arraySlice}
	value.ArrayProto["concat"] = &value.Function{Name: "concat", Arity: 1, Native: arrayConcat}
	value.ArrayProto["join"] = &value.Function{Name: "join", Arity: 1, Native: arrayJoin}
	value.ArrayProto["indexOf"] = &value.Function{Name: "indexOf", Arity: 1, Native: arrayIndexOf}
	value.ArrayProto["lastIndexOf"] = &value.Function{Name: "lastIndexOf", Arity: 1, Native: arrayLastIndexOf}
	value.ArrayProto["includes"] = &value.Function{Name: "includes", Arity: 1, Native: arrayIncludes}
	value.ArrayProto["flat"] = &value.Function{Name: "flat", Arity: 0, Native: arrayFlat}
	value.ArrayProto["at"] = &value.Function{Name: "at", Arity: 1, Native: arrayAt}
	value.ArrayProto["copyWithin"] = &value.Function{Name: "copyWithin", Arity: 2, Native: arrayCopyWithin}

	// Callback-taking iteration methods (Wave 2). They use the
	// caller.Call(fn, this, args) re-entry to invoke the user's
	// JS or native callback.
	value.ArrayProto["map"] = &value.Function{Name: "map", Arity: 1, Native: arrayMap}
	value.ArrayProto["filter"] = &value.Function{Name: "filter", Arity: 1, Native: arrayFilter}
	value.ArrayProto["forEach"] = &value.Function{Name: "forEach", Arity: 1, Native: arrayForEach}
	value.ArrayProto["reduce"] = &value.Function{Name: "reduce", Arity: 1, Native: arrayReduce}
	value.ArrayProto["reduceRight"] = &value.Function{Name: "reduceRight", Arity: 1, Native: arrayReduceRight}
	value.ArrayProto["find"] = &value.Function{Name: "find", Arity: 1, Native: arrayFind}
	value.ArrayProto["findIndex"] = &value.Function{Name: "findIndex", Arity: 1, Native: arrayFindIndex}
	value.ArrayProto["findLast"] = &value.Function{Name: "findLast", Arity: 1, Native: arrayFindLast}
	value.ArrayProto["findLastIndex"] = &value.Function{Name: "findLastIndex", Arity: 1, Native: arrayFindLastIndex}
	value.ArrayProto["toReversed"] = &value.Function{Name: "toReversed", Arity: 0, Native: arrayToReversed}
	value.ArrayProto["toSorted"] = &value.Function{Name: "toSorted", Arity: 1, Native: arrayToSorted}
	value.ArrayProto["with"] = &value.Function{Name: "with", Arity: 2, Native: arrayWith}
	value.ArrayProto["every"] = &value.Function{Name: "every", Arity: 1, Native: arrayEvery}
	value.ArrayProto["some"] = &value.Function{Name: "some", Arity: 1, Native: arraySome}
	value.ArrayProto["sort"] = &value.Function{Name: "sort", Arity: 1, Native: arraySort}
	value.ArrayProto["flatMap"] = &value.Function{Name: "flatMap", Arity: 1, Native: arrayFlatMap}
}

func arrayPush(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.push", "array")
	}
	arr := this.AsArray()
	for _, v := range args {
		arr.Push(v)
	}
	return value.Number(float64(arr.Length())), nil
}

func arrayPop(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.pop", "array")
	}
	arr := this.AsArray()
	n := arr.Length()
	if n == 0 {
		return value.Undefined(), nil
	}
	last := arr.Get(n - 1)
	arr.Truncate(n - 1)
	return last, nil
}

func arrayShift(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.shift", "array")
	}
	arr := this.AsArray()
	n := arr.Length()
	if n == 0 {
		return value.Undefined(), nil
	}
	first := arr.Get(0)
	// shift left, then truncate length by one.
	for i := 1; i < n; i++ {
		arr.Set(i-1, arr.Get(i))
	}
	arr.Truncate(n - 1)
	return first, nil
}

func arrayUnshift(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.unshift", "array")
	}
	arr := this.AsArray()
	if len(args) == 0 {
		return value.Number(float64(arr.Length())), nil
	}
	n := arr.Length()
	k := len(args)
	// Extend by k undefineds first.
	for i := 0; i < k; i++ {
		arr.Push(value.Undefined())
	}
	// Shift old elements right by k.
	for i := n - 1; i >= 0; i-- {
		arr.Set(i+k, arr.Get(i))
	}
	for i, v := range args {
		arr.Set(i, v)
	}
	return value.Number(float64(arr.Length())), nil
}

func arrayReverse(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.reverse", "array")
	}
	arr := this.AsArray()
	n := arr.Length()
	for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
		a := arr.Get(i)
		b := arr.Get(j)
		arr.Set(i, b)
		arr.Set(j, a)
	}
	return this, nil
}

func arrayFill(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.fill", "array")
	}
	arr := this.AsArray()
	n := arr.Length()
	v := argOrUndef(args, 0)
	start := 0
	if len(args) >= 2 && args[1].Type() != value.TypeUndefined {
		start = clampIndex(intArg(args, 1, 0), n)
	}
	end := n
	if len(args) >= 3 && args[2].Type() != value.TypeUndefined {
		end = clampIndex(intArg(args, 2, n), n)
	}
	for i := start; i < end; i++ {
		arr.Set(i, v)
	}
	return this, nil
}

func arraySlice(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.slice", "array")
	}
	arr := this.AsArray()
	n := arr.Length()
	start := 0
	if len(args) >= 1 && args[0].Type() != value.TypeUndefined {
		start = clampIndex(intArg(args, 0, 0), n)
	}
	end := n
	if len(args) >= 2 && args[1].Type() != value.TypeUndefined {
		end = clampIndex(intArg(args, 1, n), n)
	}
	out := value.NewArray()
	for i := start; i < end; i++ {
		out.Push(arr.Get(i))
	}
	return value.ArrayVal(out), nil
}

func arrayConcat(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.concat", "array")
	}
	arr := this.AsArray()
	out := value.NewArray()
	for i := 0; i < arr.Length(); i++ {
		out.Push(arr.Get(i))
	}
	for _, a := range args {
		if a.Type() == value.TypeArray {
			inner := a.AsArray()
			for i := 0; i < inner.Length(); i++ {
				out.Push(inner.Get(i))
			}
		} else {
			out.Push(a)
		}
	}
	return value.ArrayVal(out), nil
}

func arrayJoin(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.join", "array")
	}
	arr := this.AsArray()
	sep := ","
	if len(args) >= 1 && args[0].Type() != value.TypeUndefined {
		sep = args[0].String()
	}
	n := arr.Length()
	if n == 0 {
		return value.String(""), nil
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(sep)
		}
		v := arr.Get(i)
		// undefined / null serialize as empty string in Array#join.
		t := v.Type()
		if t == value.TypeUndefined || t == value.TypeNull {
			continue
		}
		b.WriteString(v.String())
	}
	return value.String(b.String()), nil
}

func arrayIndexOf(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.indexOf", "array")
	}
	arr := this.AsArray()
	n := arr.Length()
	target := argOrUndef(args, 0)
	from := 0
	if len(args) >= 2 {
		from = intArg(args, 1, 0)
		if from < 0 {
			from += n
			if from < 0 {
				from = 0
			}
		}
	}
	for i := from; i < n; i++ {
		if strictEqual(arr.Get(i), target) {
			return value.Number(float64(i)), nil
		}
	}
	return value.Number(-1), nil
}

func arrayLastIndexOf(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.lastIndexOf", "array")
	}
	arr := this.AsArray()
	n := arr.Length()
	target := argOrUndef(args, 0)
	from := n - 1
	if len(args) >= 2 {
		from = intArg(args, 1, n-1)
		if from < 0 {
			from += n
		}
		if from >= n {
			from = n - 1
		}
	}
	for i := from; i >= 0; i-- {
		if strictEqual(arr.Get(i), target) {
			return value.Number(float64(i)), nil
		}
	}
	return value.Number(-1), nil
}

func arrayIncludes(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.includes", "array")
	}
	arr := this.AsArray()
	n := arr.Length()
	target := argOrUndef(args, 0)
	from := 0
	if len(args) >= 2 {
		from = intArg(args, 1, 0)
		if from < 0 {
			from += n
			if from < 0 {
				from = 0
			}
		}
	}
	for i := from; i < n; i++ {
		if sameValueZero(arr.Get(i), target) {
			return value.Bool(true), nil
		}
	}
	return value.Bool(false), nil
}

func arrayFlat(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.flat", "array")
	}
	depth := 1
	if len(args) >= 1 && args[0].Type() != value.TypeUndefined {
		depth = intArg(args, 0, 1)
	}
	out := value.NewArray()
	flattenInto(out, this.AsArray(), depth)
	return value.ArrayVal(out), nil
}

// arrayAt returns the element at idx, supporting negative indices
// that count from the end (Array.prototype.at).
func arrayAt(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.at", "array")
	}
	arr := this.AsArray()
	n := arr.Length()
	i := intArg(args, 0, 0)
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return value.Undefined(), nil
	}
	return arr.Get(i), nil
}

// arrayCopyWithin copies a sequence of elements within the array,
// in place. Signature: copyWithin(target, start=0, end=length).
// Returns the same array (mutated).
func arrayCopyWithin(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.copyWithin", "array")
	}
	arr := this.AsArray()
	n := arr.Length()
	target := clampIndex(intArg(args, 0, 0), n)
	start := 0
	if len(args) >= 2 && args[1].Type() != value.TypeUndefined {
		start = clampIndex(intArg(args, 1, 0), n)
	}
	end := n
	if len(args) >= 3 && args[2].Type() != value.TypeUndefined {
		end = clampIndex(intArg(args, 2, n), n)
	}
	count := end - start
	if count <= 0 {
		return this, nil
	}
	if target+count > n {
		count = n - target
	}
	if count <= 0 {
		return this, nil
	}
	// Snapshot the source slice first so overlapping copies behave.
	src := make([]value.Value, count)
	for i := 0; i < count; i++ {
		src[i] = arr.Get(start + i)
	}
	for i := 0; i < count; i++ {
		arr.Set(target+i, src[i])
	}
	return this, nil
}

// ----- internal helpers ------------------------------------------------


// flattenInto recursively appends src into dst, descending up to depth
// levels into nested arrays.
func flattenInto(dst *value.Array, src *value.Array, depth int) {
	for i := 0; i < src.Length(); i++ {
		v := src.Get(i)
		if depth > 0 && v.Type() == value.TypeArray {
			flattenInto(dst, v.AsArray(), depth-1)
		} else {
			dst.Push(v)
		}
	}
}

// clampIndex normalises a JS index argument against length n,
// resolving negatives by adding n and clamping to [0, n].
func clampIndex(i, n int) int {
	if i < 0 {
		i += n
		if i < 0 {
			return 0
		}
		return i
	}
	if i > n {
		return n
	}
	return i
}

// strictEqual mirrors the === semantics for the types Array elements
// can carry. NaN !== NaN.
func strictEqual(a, b value.Value) bool {
	if a.Type() != b.Type() {
		return false
	}
	switch a.Type() {
	case value.TypeUndefined, value.TypeNull:
		return true
	case value.TypeBool, value.TypeNumber:
		af, bf := a.AsNumber(), b.AsNumber()
		if math.IsNaN(af) || math.IsNaN(bf) {
			return false
		}
		return af == bf
	case value.TypeString:
		return a.AsString() == b.AsString()
	}
	// Reference types: same pointer identity (not exposed cheaply, so
	// fall back to false until we need it).
	return false
}

// sameValueZero is strictEqual but NaN equals NaN — the algorithm
// Array#includes uses.
func sameValueZero(a, b value.Value) bool {
	if a.Type() != b.Type() {
		return false
	}
	if a.Type() == value.TypeNumber {
		af, bf := a.AsNumber(), b.AsNumber()
		if math.IsNaN(af) && math.IsNaN(bf) {
			return true
		}
		return af == bf
	}
	return strictEqual(a, b)
}

// ----- callback-taking methods --------------------------------------
//
// All of these invoke the user's callback through caller.Call(fn,
// this, args). The receiver of the callback is undefined (we do not
// yet implement the optional thisArg). The standard arg order
// passed to the callback is (element, index, array).

// callbackPair extracts the array receiver and the callback function
// once per method so the per-method body stays short.
func callbackPair(this value.Value, args []value.Value, method string) (*value.Array, *value.Function, error) {
	if this.Type() != value.TypeArray {
		return nil, nil, badThis("Array.prototype."+method, "array")
	}
	cb := argOrUndef(args, 0)
	if cb.Type() != value.TypeFunction {
		return nil, nil, badThis("Array.prototype."+method, "function callback")
	}
	return this.AsArray(), cb.AsFunction(), nil
}

func arrayMap(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	arr, fn, err := callbackPair(this, args, "map")
	if err != nil {
		return value.Value{}, err
	}
	out := value.NewArray()
	for i := 0; i < arr.Length(); i++ {
		result, err := caller.Call(fn, value.Undefined(), []value.Value{
			arr.Get(i), value.Number(float64(i)), this,
		})
		if err != nil {
			return value.Value{}, err
		}
		out.Push(result)
	}
	return value.ArrayVal(out), nil
}

func arrayFilter(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	arr, fn, err := callbackPair(this, args, "filter")
	if err != nil {
		return value.Value{}, err
	}
	out := value.NewArray()
	for i := 0; i < arr.Length(); i++ {
		item := arr.Get(i)
		result, err := caller.Call(fn, value.Undefined(), []value.Value{
			item, value.Number(float64(i)), this,
		})
		if err != nil {
			return value.Value{}, err
		}
		if isTruthy(result) {
			out.Push(item)
		}
	}
	return value.ArrayVal(out), nil
}

func arrayForEach(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	arr, fn, err := callbackPair(this, args, "forEach")
	if err != nil {
		return value.Value{}, err
	}
	for i := 0; i < arr.Length(); i++ {
		_, err := caller.Call(fn, value.Undefined(), []value.Value{
			arr.Get(i), value.Number(float64(i)), this,
		})
		if err != nil {
			return value.Value{}, err
		}
	}
	return value.Undefined(), nil
}

func arrayReduce(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	arr, fn, err := callbackPair(this, args, "reduce")
	if err != nil {
		return value.Value{}, err
	}
	n := arr.Length()
	var acc value.Value
	start := 0
	if len(args) >= 2 {
		acc = args[1]
	} else {
		if n == 0 {
			return value.Value{}, &typeError{msg: "Reduce of empty array with no initial value"}
		}
		acc = arr.Get(0)
		start = 1
	}
	for i := start; i < n; i++ {
		result, err := caller.Call(fn, value.Undefined(), []value.Value{
			acc, arr.Get(i), value.Number(float64(i)), this,
		})
		if err != nil {
			return value.Value{}, err
		}
		acc = result
	}
	return acc, nil
}

func arrayReduceRight(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	arr, fn, err := callbackPair(this, args, "reduceRight")
	if err != nil {
		return value.Value{}, err
	}
	n := arr.Length()
	var acc value.Value
	start := n - 1
	if len(args) >= 2 {
		acc = args[1]
	} else {
		if n == 0 {
			return value.Value{}, &typeError{msg: "Reduce of empty array with no initial value"}
		}
		acc = arr.Get(n - 1)
		start = n - 2
	}
	for i := start; i >= 0; i-- {
		result, err := caller.Call(fn, value.Undefined(), []value.Value{
			acc, arr.Get(i), value.Number(float64(i)), this,
		})
		if err != nil {
			return value.Value{}, err
		}
		acc = result
	}
	return acc, nil
}

func arrayFind(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	arr, fn, err := callbackPair(this, args, "find")
	if err != nil {
		return value.Value{}, err
	}
	for i := 0; i < arr.Length(); i++ {
		item := arr.Get(i)
		result, err := caller.Call(fn, value.Undefined(), []value.Value{
			item, value.Number(float64(i)), this,
		})
		if err != nil {
			return value.Value{}, err
		}
		if isTruthy(result) {
			return item, nil
		}
	}
	return value.Undefined(), nil
}

func arrayFindIndex(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	arr, fn, err := callbackPair(this, args, "findIndex")
	if err != nil {
		return value.Value{}, err
	}
	for i := 0; i < arr.Length(); i++ {
		result, err := caller.Call(fn, value.Undefined(), []value.Value{
			arr.Get(i), value.Number(float64(i)), this,
		})
		if err != nil {
			return value.Value{}, err
		}
		if isTruthy(result) {
			return value.Number(float64(i)), nil
		}
	}
	return value.Number(-1), nil
}

// arrayFindLast / arrayFindLastIndex iterate from the tail; otherwise
// identical to find / findIndex. Useful for "most-recent" lookups.
func arrayFindLast(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	arr, fn, err := callbackPair(this, args, "findLast")
	if err != nil {
		return value.Value{}, err
	}
	for i := arr.Length() - 1; i >= 0; i-- {
		item := arr.Get(i)
		result, err := caller.Call(fn, value.Undefined(), []value.Value{
			item, value.Number(float64(i)), this,
		})
		if err != nil {
			return value.Value{}, err
		}
		if isTruthy(result) {
			return item, nil
		}
	}
	return value.Undefined(), nil
}

// arrayToReversed returns a new array containing this's elements in
// reverse order. Non-mutating counterpart of reverse().
func arrayToReversed(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.toReversed", "Array")
	}
	src := this.AsArray()
	n := src.Length()
	out := value.NewArrayWithCap(n)
	for i := n - 1; i >= 0; i-- {
		out.Push(src.Get(i))
	}
	return value.ArrayVal(out), nil
}

// arrayToSorted returns a new sorted array; this is untouched. Reuses
// the same compare-callback logic as sort().
func arrayToSorted(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.toSorted", "Array")
	}
	src := this.AsArray()
	n := src.Length()
	out := value.NewArrayWithCap(n)
	for i := 0; i < n; i++ {
		out.Push(src.Get(i))
	}
	if _, err := arraySort(caller, value.ArrayVal(out), args); err != nil {
		return value.Value{}, err
	}
	return value.ArrayVal(out), nil
}

// arrayWith returns a new array with index replaced by value;
// throws RangeError when the index (after negative normalisation)
// is out of bounds.
func arrayWith(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.with", "Array")
	}
	src := this.AsArray()
	n := src.Length()
	idx := intArg(args, 0, 0)
	if idx < 0 {
		idx += n
	}
	if idx < 0 || idx >= n {
		return value.Value{}, &value.JSThrow{Val: makeError("RangeError", "Array.prototype.with: index out of range")}
	}
	v := argOrUndef(args, 1)
	out := value.NewArrayWithCap(n)
	for i := 0; i < n; i++ {
		if i == idx {
			out.Push(v)
		} else {
			out.Push(src.Get(i))
		}
	}
	return value.ArrayVal(out), nil
}

func arrayFindLastIndex(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	arr, fn, err := callbackPair(this, args, "findLastIndex")
	if err != nil {
		return value.Value{}, err
	}
	for i := arr.Length() - 1; i >= 0; i-- {
		result, err := caller.Call(fn, value.Undefined(), []value.Value{
			arr.Get(i), value.Number(float64(i)), this,
		})
		if err != nil {
			return value.Value{}, err
		}
		if isTruthy(result) {
			return value.Number(float64(i)), nil
		}
	}
	return value.Number(-1), nil
}

func arrayEvery(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	arr, fn, err := callbackPair(this, args, "every")
	if err != nil {
		return value.Value{}, err
	}
	for i := 0; i < arr.Length(); i++ {
		result, err := caller.Call(fn, value.Undefined(), []value.Value{
			arr.Get(i), value.Number(float64(i)), this,
		})
		if err != nil {
			return value.Value{}, err
		}
		if !isTruthy(result) {
			return value.Bool(false), nil
		}
	}
	return value.Bool(true), nil
}

func arraySome(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	arr, fn, err := callbackPair(this, args, "some")
	if err != nil {
		return value.Value{}, err
	}
	for i := 0; i < arr.Length(); i++ {
		result, err := caller.Call(fn, value.Undefined(), []value.Value{
			arr.Get(i), value.Number(float64(i)), this,
		})
		if err != nil {
			return value.Value{}, err
		}
		if isTruthy(result) {
			return value.Bool(true), nil
		}
	}
	return value.Bool(false), nil
}

// arrayFlatMap is map+flat(1) in one pass. The callback can return
// either a plain value (appended as-is) or an array (its elements
// are spread into the result).
func arrayFlatMap(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	arr, fn, err := callbackPair(this, args, "flatMap")
	if err != nil {
		return value.Value{}, err
	}
	out := value.NewArray()
	for i := 0; i < arr.Length(); i++ {
		result, err := caller.Call(fn, value.Undefined(), []value.Value{
			arr.Get(i), value.Number(float64(i)), this,
		})
		if err != nil {
			return value.Value{}, err
		}
		if result.Type() == value.TypeArray {
			inner := result.AsArray()
			for j := 0; j < inner.Length(); j++ {
				out.Push(inner.Get(j))
			}
		} else {
			out.Push(result)
		}
	}
	return value.ArrayVal(out), nil
}

// arraySort is the only callback method whose callback is OPTIONAL.
// With no comparator the spec calls for ToString-then-lexicographic;
// with a comparator the callback returns a number whose sign decides
// order. Sort is in-place and returns this.
//
// We copy items into a Go slice to feed sort.SliceStable, so a
// callback that mutates the array mid-sort doesn't poison comparisons
// (real spec is more careful but no corpus stresses it).
func arraySort(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeArray {
		return value.Value{}, badThis("Array.prototype.sort", "array")
	}
	arr := this.AsArray()
	var fn *value.Function
	cmp := argOrUndef(args, 0)
	if cmp.Type() == value.TypeFunction {
		fn = cmp.AsFunction()
	}
	items := make([]value.Value, arr.Length())
	for i := range items {
		items[i] = arr.Get(i)
	}
	var sortErr error
	sort.SliceStable(items, func(i, j int) bool {
		if sortErr != nil {
			return false
		}
		if fn == nil {
			return items[i].String() < items[j].String()
		}
		result, err := caller.Call(fn, value.Undefined(), []value.Value{items[i], items[j]})
		if err != nil {
			sortErr = err
			return false
		}
		return result.AsNumber() < 0
	})
	if sortErr != nil {
		return value.Value{}, sortErr
	}
	for i, v := range items {
		arr.Set(i, v)
	}
	return this, nil
}
