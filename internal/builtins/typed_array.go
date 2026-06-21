// ArrayBuffer + TypedArray (Uint8Array / Int32Array / Float64Array).
//
// Implementation is byte-level: the buffer is a Go []byte, each
// typed-array instance carries (elemType, byteOffset, length) plus
// a pointer to the buffer's storage. Index access goes through
// custom slots on the wrapping Object so `arr[0]` and `arr[0] = 5`
// route via vm.getByVal / setByVal — those code paths fast-path
// numeric keys on Arrays today, and TypedArray instances reuse
// that path by wrapping the byte storage as a value.Array of
// numbers.

package builtins

import (
	"encoding/binary"
	"math"

	"github.com/vimt/goquickjs/internal/value"
)

// abInternals keeps the raw byte storage off the user-visible
// surface. Keyed by the wrapping *Object so two distinct buffers
// stay distinct.
var abStorage = map[*value.Object][]byte{}

type taKind int

const (
	taUint8 taKind = iota
	taInt32
	taFloat64
)

func (k taKind) bytesPerElem() int {
	switch k {
	case taUint8:
		return 1
	case taInt32:
		return 4
	case taFloat64:
		return 8
	}
	return 0
}

// taInternals records the typed-array view's element kind plus
// offset/length into the backing buffer.
type taInfo struct {
	kind   taKind
	buffer *value.Object
	offset int
	length int
}

var taViews = map[*value.Object]*taInfo{}

func installTypedArrays(globals map[string]value.Value) {
	// ArrayBuffer(length)
	abFn := &value.Function{Name: "ArrayBuffer", Arity: 1, Native: arrayBufferConstruct}
	globals["ArrayBuffer"] = value.FunctionVal(abFn)

	globals["Uint8Array"] = makeTypedArrayCtor("Uint8Array", taUint8)
	globals["Int32Array"] = makeTypedArrayCtor("Int32Array", taInt32)
	globals["Float64Array"] = makeTypedArrayCtor("Float64Array", taFloat64)
}

func arrayBufferConstruct(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	n := intArg(args, 0, 0)
	if n < 0 {
		return value.Value{}, &value.JSThrow{Val: makeError("RangeError", "ArrayBuffer: negative length")}
	}
	buf := value.NewObject()
	abStorage[buf] = make([]byte, n)
	buf.Set("byteLength", value.Number(float64(n)))
	return value.ObjectVal(buf), nil
}

func makeTypedArrayCtor(name string, kind taKind) value.Value {
	fn := &value.Function{
		Name:  name,
		Arity: 1,
		Native: func(c value.Caller, this value.Value, args []value.Value) (value.Value, error) {
			return typedArrayConstruct(kind, args)
		},
	}
	return value.FunctionVal(fn)
}

// typedArrayConstruct handles all the spec's overloads we care
// about:
//
//	new TA(length)          — fresh zeroed buffer
//	new TA(arrayLike)       — copy from an Array / TypedArray
//	new TA(buffer, offset, length) — view into existing ArrayBuffer
func typedArrayConstruct(kind taKind, args []value.Value) (value.Value, error) {
	a0 := argOrUndef(args, 0)
	switch a0.Type() {
	case value.TypeNumber, value.TypeUndefined:
		n := intArg(args, 0, 0)
		buf := value.NewObject()
		abStorage[buf] = make([]byte, n*kind.bytesPerElem())
		buf.Set("byteLength", value.Number(float64(n*kind.bytesPerElem())))
		return makeTypedArrayView(kind, buf, 0, n), nil
	case value.TypeArray:
		src := a0.AsArray()
		n := src.Length()
		buf := value.NewObject()
		abStorage[buf] = make([]byte, n*kind.bytesPerElem())
		buf.Set("byteLength", value.Number(float64(n*kind.bytesPerElem())))
		v := makeTypedArrayView(kind, buf, 0, n)
		store := abStorage[buf]
		for i := 0; i < n; i++ {
			writeElem(store, kind, i, src.Get(i).AsNumber())
		}
		return v, nil
	case value.TypeObject:
		// ArrayBuffer view if it carries our storage marker.
		if _, ok := abStorage[a0.AsObject()]; ok {
			offset := intArg(args, 1, 0)
			storage := abStorage[a0.AsObject()]
			length := (len(storage) - offset) / kind.bytesPerElem()
			if len(args) >= 3 {
				length = intArg(args, 2, 0)
			}
			return makeTypedArrayView(kind, a0.AsObject(), offset, length), nil
		}
	}
	return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "TypedArray: unsupported argument")}
}

func makeTypedArrayView(kind taKind, buf *value.Object, offset, length int) value.Value {
	view := value.NewObject()
	taViews[view] = &taInfo{kind: kind, buffer: buf, offset: offset, length: length}
	view.Set("length", value.Number(float64(length)))
	view.Set("byteOffset", value.Number(float64(offset)))
	view.Set("byteLength", value.Number(float64(length*kind.bytesPerElem())))
	view.Set("buffer", value.ObjectVal(buf))
	view.SetAccessor("__elements__", &value.Function{Name: "elems", Arity: 0,
		Native: func(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
			return readAllElems(view), nil
		}}, nil)
	// Expose set + slice; index access goes through Object property
	// reads since we mirror values into named slots "0", "1", etc.
	view.Set("set", value.FunctionVal(&value.Function{
		Name: "set", Arity: 2,
		Native: func(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
			return typedArraySet(view, args)
		},
	}))
	view.Set("slice", value.FunctionVal(&value.Function{
		Name: "slice", Arity: 2,
		Native: func(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
			return typedArraySlice(view, args)
		},
	}))
	// Wire the indexed read/write hooks so `a[i] = v` truncates
	// per element-kind and `a[i]` always reflects the live bytes.
	infoCap := taViews[view]
	view.IndexedWrite = func(idx int, v value.Value) bool {
		if idx < 0 || idx >= infoCap.length {
			return true // out-of-range silently ignored, spec-ish
		}
		writeElem(abStorage[infoCap.buffer], infoCap.kind, idx+infoCap.offset/infoCap.kind.bytesPerElem(), v.AsNumber())
		return true
	}
	view.IndexedRead = func(idx int) (value.Value, bool) {
		if idx < 0 || idx >= infoCap.length {
			return value.Undefined(), false
		}
		return value.Number(readElem(abStorage[infoCap.buffer], infoCap.kind, idx+infoCap.offset/infoCap.kind.bytesPerElem())), true
	}
	return value.ObjectVal(view)
}

func readAllElems(view *value.Object) value.Value {
	info := taViews[view]
	if info == nil {
		return value.Undefined()
	}
	out := value.NewArrayWithCap(info.length)
	for i := 0; i < info.length; i++ {
		out.Push(value.Number(readElem(abStorage[info.buffer], info.kind, i+info.offset/info.kind.bytesPerElem())))
	}
	return value.ArrayVal(out)
}

func typedArraySet(view *value.Object, args []value.Value) (value.Value, error) {
	info := taViews[view]
	if info == nil {
		return value.Undefined(), nil
	}
	src := argOrUndef(args, 0)
	off := intArg(args, 1, 0)
	if src.Type() == value.TypeArray {
		a := src.AsArray()
		store := abStorage[info.buffer]
		for i := 0; i < a.Length(); i++ {
			writeElem(store, info.kind, info.offset/info.kind.bytesPerElem()+off+i, a.Get(i).AsNumber())
			view.Set(intToKey(off+i), a.Get(i))
		}
	}
	return value.Undefined(), nil
}

func typedArraySlice(view *value.Object, args []value.Value) (value.Value, error) {
	info := taViews[view]
	if info == nil {
		return value.Undefined(), nil
	}
	start := intArg(args, 0, 0)
	end := intArg(args, 1, info.length)
	if start < 0 {
		start += info.length
	}
	if end < 0 {
		end += info.length
	}
	if start < 0 {
		start = 0
	}
	if end > info.length {
		end = info.length
	}
	n := end - start
	if n < 0 {
		n = 0
	}
	buf := value.NewObject()
	abStorage[buf] = make([]byte, n*info.kind.bytesPerElem())
	v := makeTypedArrayView(info.kind, buf, 0, n)
	store := abStorage[info.buffer]
	dst := abStorage[buf]
	for i := 0; i < n; i++ {
		writeElem(dst, info.kind, i, readElem(store, info.kind, info.offset/info.kind.bytesPerElem()+start+i))
	}
	// Refresh the shape mirror.
	view2 := v.AsObject()
	for i := 0; i < n; i++ {
		view2.Set(intToKey(i), value.Number(readElem(dst, info.kind, i)))
	}
	return v, nil
}

func readElem(buf []byte, kind taKind, i int) float64 {
	switch kind {
	case taUint8:
		if i < 0 || i >= len(buf) {
			return 0
		}
		return float64(buf[i])
	case taInt32:
		off := i * 4
		if off+4 > len(buf) {
			return 0
		}
		return float64(int32(binary.LittleEndian.Uint32(buf[off:])))
	case taFloat64:
		off := i * 8
		if off+8 > len(buf) {
			return 0
		}
		bits := binary.LittleEndian.Uint64(buf[off:])
		return math.Float64frombits(bits)
	}
	return 0
}

func writeElem(buf []byte, kind taKind, i int, v float64) {
	switch kind {
	case taUint8:
		if i < 0 || i >= len(buf) {
			return
		}
		buf[i] = byte(toUint32JS(v) & 0xff)
	case taInt32:
		off := i * 4
		if off+4 > len(buf) {
			return
		}
		binary.LittleEndian.PutUint32(buf[off:], toUint32JS(v))
	case taFloat64:
		off := i * 8
		if off+8 > len(buf) {
			return
		}
		binary.LittleEndian.PutUint64(buf[off:], math.Float64bits(v))
	}
}

// toUint32JS implements the ES ToUint32 abstract op: NaN/Inf→0, then
// truncate toward zero, then mod 2^32. Used to feed Int32/Uint8 view
// writes the same wrap-around semantics real engines apply.
func toUint32JS(f float64) uint32 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	if f >= 0 {
		f = math.Floor(f)
	} else {
		f = math.Ceil(f)
	}
	// Reduce mod 2^32 in float space so very large numbers don't
	// overflow the int conversion.
	mod := math.Mod(f, 4294967296.0)
	if mod < 0 {
		mod += 4294967296.0
	}
	return uint32(mod)
}

