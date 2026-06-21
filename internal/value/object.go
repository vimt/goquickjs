package value

import "strings"

// Shape is an immutable, shareable layout descriptor.
type Shape struct {
	propsByName map[string]int // name → slot index
	propNames   []string       // ordered list (insertion order)
	transitions map[string]*Shape
}

// emptyShape is the root every fresh object starts from.
var emptyShape = &Shape{
	propsByName: map[string]int{},
	transitions: map[string]*Shape{},
}

// PropIndex returns the slot for name, or -1 if absent.
func (s *Shape) PropIndex(name string) int {
	if idx, ok := s.propsByName[name]; ok {
		return idx
	}
	return -1
}

// Extend returns the shape obtained by appending name. Cached on the
// parent so two objects taking the same construction path share it.
func (s *Shape) Extend(name string) *Shape {
	if next, ok := s.transitions[name]; ok {
		return next
	}
	next := &Shape{
		propsByName: make(map[string]int, len(s.propsByName)+1),
		propNames:   make([]string, len(s.propNames)+1),
		transitions: map[string]*Shape{},
	}
	for k, v := range s.propsByName {
		next.propsByName[k] = v
	}
	copy(next.propNames, s.propNames)
	next.propsByName[name] = len(s.propNames)
	next.propNames[len(s.propNames)] = name
	s.transitions[name] = next
	return next
}

// Object is a Shape pointer plus a parallel slot vector, plus an
// optional prototype pointer for [[GetPrototypeOf]] / chain lookups.
//
// deleted is a lazy tombstone set: rather than tear down the shape on
// every `delete obj.x` (which would force the value vector to slide
// and break shape sharing), we mark the slot tombstoned and let
// GetOwn / PropNames filter it out. A subsequent Set with the same
// name clears the tombstone in-place.
type Object struct {
	shape   *Shape
	values  []Value
	proto   *Object         // [[Prototype]]; nil = no prototype chain
	deleted map[string]bool // tombstones; nil when none
	// accessors hold getter/setter pairs installed via
	// Object.defineProperty or a class/object-literal `get name(){}`.
	// Looked up before falling back to the shape-stored data slot.
	accessors map[string]*Accessor
	// proxy, when non-nil, makes the VM route property operations
	// through the handler's traps. The Object's own values are
	// ignored on the read/write path (handler.get/set are authoritative).
	Proxy *ProxyMeta
	// Indexed read/write hooks for TypedArray-like objects. nil on
	// plain objects.
	IndexedWrite IndexedWriter
	IndexedRead  IndexedReader
}

// ProxyMeta is the internal record `new Proxy(target, handler)` sets
// on the resulting Object. Trap fns live on Handler; Target is the
// underlying object trap fns receive as their first argument.
type ProxyMeta struct {
	Target  *Object
	Handler *Object
}

// Accessor is one ES property descriptor with get/set functions.
// Either may be nil (just-getter or just-setter); both nil is
// equivalent to a deleted accessor.
type Accessor struct {
	Get *Function
	Set *Function
}

// IndexedWriter, when non-nil on an Object, intercepts numeric
// property writes — used by TypedArray to coerce the incoming
// number through the element-kind truncation rules before storing.
// Returns true if the write was fully handled (no fallback to the
// normal Set path).
type IndexedWriter func(idx int, v Value) bool

// IndexedReader is the symmetric read hook — used by TypedArray to
// project the stored bytes back to a JS number. Returns (val, true)
// when the index belongs to the view; (Undefined, false) hands the
// read back to the normal property dispatch.
type IndexedReader func(idx int) (Value, bool)

// SetAccessor installs (or replaces) the accessor pair for name.
// Removes any data-slot value at the same name so reads route
// through the getter.
func (o *Object) SetAccessor(name string, getter, setter *Function) {
	if o.accessors == nil {
		o.accessors = map[string]*Accessor{}
	}
	o.accessors[name] = &Accessor{Get: getter, Set: setter}
}

// Accessor returns the accessor descriptor for name, or nil.
func (o *Object) Accessor(name string) *Accessor {
	if o.accessors == nil {
		return nil
	}
	return o.accessors[name]
}

// LookupAccessor walks the prototype chain looking for an accessor
// at name. The receiver isn't bound here — callers invoke a found
// getter/setter with the original target as `this`.
func (o *Object) LookupAccessor(name string) *Accessor {
	for p := o; p != nil; p = p.proto {
		if a := p.Accessor(name); a != nil {
			return a
		}
	}
	return nil
}

// ObjectPrototype is the singleton every plain Object inherits from
// by default. builtins.installObject hangs hasOwnProperty / toString
// / valueOf / isPrototypeOf / propertyIsEnumerable on it at init time,
// so `({}).hasOwnProperty('a')` resolves up the chain to a real method.
// It is itself a "bare" object — its own proto is nil to terminate the
// chain.
var ObjectPrototype = &Object{shape: emptyShape}

// NewObject returns an empty object inheriting from ObjectPrototype.
// Use NewBareObject for chain-terminating cases like Object.create(null)
// or ObjectPrototype itself.
func NewObject() *Object {
	return &Object{shape: emptyShape, proto: ObjectPrototype}
}

// NewBareObject returns an empty object with proto = nil. Reserve for
// prototype roots, Object.create(null), and any container that must
// not carry inherited Object.prototype methods (e.g. descriptor maps
// where extra keys would leak via `in`/`hasOwnProperty`).
func NewBareObject() *Object {
	return &Object{shape: emptyShape}
}

func (o *Object) Shape() *Shape      { return o.shape }
func (o *Object) Proto() *Object     { return o.proto }
func (o *Object) SetProto(p *Object) { o.proto = p }

// PropNames returns own property names in insertion order, skipping
// any that have been tombstoned via Delete. The fast path (no
// deletions) hands back the shape's slice directly with no copy.
func (o *Object) PropNames() []string {
	if len(o.deleted) == 0 {
		return o.shape.propNames
	}
	out := make([]string, 0, len(o.shape.propNames))
	for _, n := range o.shape.propNames {
		if !o.deleted[n] {
			out = append(out, n)
		}
	}
	return out
}

// Get returns (value, true) for an own property; for inherited
// properties it walks the prototype chain. The bool reports own-ness
// only — callers that need to distinguish should use GetOwn instead.
func (o *Object) Get(name string) (Value, bool) {
	if v, ok := o.GetOwn(name); ok {
		return v, true
	}
	for p := o.proto; p != nil; p = p.proto {
		if v, ok := p.GetOwn(name); ok {
			return v, false
		}
	}
	return Undefined(), false
}

// GetOwn returns the value of an own property only, never walking the
// prototype chain. Used by builtins like Object.hasOwnProperty.
func (o *Object) GetOwn(name string) (Value, bool) {
	if o.deleted[name] {
		return Undefined(), false
	}
	idx := o.shape.PropIndex(name)
	if idx < 0 {
		return Undefined(), false
	}
	return o.values[idx], true
}

// Set writes name=v, transitioning shape if name is new. A pending
// tombstone for the same name is cleared so the prop reappears.
func (o *Object) Set(name string, v Value) {
	if o.deleted[name] {
		delete(o.deleted, name)
	}
	if idx := o.shape.PropIndex(name); idx >= 0 {
		o.values[idx] = v
		return
	}
	o.shape = o.shape.Extend(name)
	o.values = append(o.values, v)
}

// Delete tombstones an own property. Returns true if the property was
// present (and is now hidden), false if it was already missing. The
// underlying slot is kept so shape identity is preserved across calls.
func (o *Object) Delete(name string) bool {
	if o.deleted[name] {
		return false
	}
	if o.shape.PropIndex(name) < 0 {
		return false
	}
	if o.deleted == nil {
		o.deleted = map[string]bool{}
	}
	o.deleted[name] = true
	return true
}

// stringify renders the object as QuickJS's REPL does: JSON-style,
// no whitespace, insertion order.
func (o *Object) stringify() string {
	if len(o.shape.propNames) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, name := range o.shape.propNames {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(jsonEscape(name))
		b.WriteString(`":`)
		b.WriteString(o.values[i].stringifyForJSON())
	}
	b.WriteByte('}')
	return b.String()
}
