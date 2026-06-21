package goquickjs

import "testing"

// These tests pin the correctness of the OpGetProp / OpSetProp inline
// caches. The caches are transparent, so every assertion is on the
// JS-level result: each script deliberately drives the SAME bytecode
// site repeatedly across changing shapes / tombstones to trip the
// cache, then checks the value the cache must still produce.

func evalEq(t *testing.T, src, want string) {
	t.Helper()
	got, err := Eval(src)
	if err != nil {
		t.Fatalf("eval error: %v\nsrc: %s", err, src)
	}
	if got != want {
		t.Fatalf("got %q want %q\nsrc: %s", got, want, src)
	}
}

// A read site that cached a slot for x must NOT return the stale value
// after the prop is deleted — the tombstone path has to win.
func TestICDeleteThenReadIsUndefined(t *testing.T) {
	evalEq(t, `
		function f(o){ return o.x; }
		var o = { x: 1, y: 2 };
		var a = f(o);     // cache fills x -> slot 0
		delete o.x;
		var b = f(o);     // must be undefined, not stale 1
		a + "," + b;
	`, "1,undefined")
}

// Delete then re-add clears the tombstone and reuses the slot; the read
// site must observe the new value.
func TestICDeleteReAdd(t *testing.T) {
	evalEq(t, `
		function f(o){ return o.x; }
		var o = { x: 1 };
		f(o);
		delete o.x;
		o.x = 5;
		f(o);
	`, "5")
}

// The critical monomorphic-thrash case: one read site alternates
// between two shapes where the SAME name lives at different slots.
// A naive cache that trusted its slot blindly would read the wrong
// field for one of them.
func TestICPolymorphicReadStaysCorrect(t *testing.T) {
	evalEq(t, `
		function getv(o){ return o.v; }
		var a = { v: 10 };           // v at slot 0
		var b = { pad: 0, v: 20 };   // v at slot 1
		var sum = 0;
		for (var i = 0; i < 50; i++) sum += getv(a) + getv(b);
		sum;
	`, "1500")
}

// Same thrash, write side: a write site alternates shapes; each object
// must end up with the value written to it, not its neighbour's slot.
func TestICPolymorphicWriteStaysCorrect(t *testing.T) {
	evalEq(t, `
		function setv(o, x){ o.v = x; }
		var a = { v: 0 };
		var b = { pad: 0, v: 0 };
		for (var i = 0; i < 50; i++) { setv(a, 7); setv(b, 9); }
		a.v + "," + b.v;
	`, "7,9")
}

// One write site mixes the add regime (fresh object, prop absent) and
// the update regime (prop already present) across calls.
func TestICWriteAddThenUpdateSameSite(t *testing.T) {
	evalEq(t, `
		function setX(o){ o.x = 1; }
		var a = {};         // add regime: empty -> {x}
		var b = { x: 99 };  // update regime: {x} already
		setX(a); setX(b);
		a.x + "," + b.x;
	`, "1,1")
}

// Add regime under a hot loop: every {} then o.a/o.b builds the same
// shape via the same transitions; the appended slots must hold the
// right values. sum_{i=0..9} (i*10 + (i+1)) = 450 + 55 = 505.
func TestICAddRegimeLoop(t *testing.T) {
	evalEq(t, `
		function mk(i){ var o = {}; o.a = i; o.b = i + 1; return o.a * 10 + o.b; }
		var s = 0;
		for (var i = 0; i < 10; i++) s += mk(i);
		s;
	`, "505")
}

// An inherited property must fall through the own-property cache to the
// prototype walk; once shadowed by an own write, the own value wins.
func TestICInheritedFallthrough(t *testing.T) {
	evalEq(t, `
		function Base(){}
		Base.prototype.greet = "hi";
		function read(o){ return o.greet; }
		var x = new Base();
		var a = read(x);   // own miss -> proto "hi"
		x.greet = "yo";    // shadow with own prop
		var b = read(x);   // own "yo"
		a + "," + b;
	`, "hi,yo")
}

// A read site sees the object grow a new field between calls: a miss
// (name absent, nothing cached) then a hit after the prop appears.
func TestICShapeGrowthAtReadSite(t *testing.T) {
	evalEq(t, `
		function ry(o){ return o.y; }
		var o = { x: 1 };
		var a = ry(o);   // y absent -> undefined
		o.y = 7;
		var b = ry(o);   // y now slot 1
		a + "," + b;
	`, "undefined,7")
}

// An own data property must shadow an inherited accessor of the same
// name: reading it returns the data value, NOT the prototype's getter.
// (This also guards the accessor-walk reorder on the read fast path.)
func TestICOwnDataShadowsInheritedGetter(t *testing.T) {
	evalEq(t, `
		function Base(){}
		Object.defineProperty(Base.prototype, "x", { get: function(){ return 9; } });
		function read(o){ return o.x; }
		var a = new Base();
		var before = read(a);                       // inherited getter -> 9
		Object.defineProperty(a, "x", { value: 5 }); // own data shadows
		var after = read(a);                        // own data -> 5
		before + "," + after;
	`, "9,5")
}

// An own getter still wins over its own data slot (the two can coexist
// in storage), so the reorder must consult own accessors first.
func TestICOwnGetterWinsOverOwnData(t *testing.T) {
	evalEq(t, `
		var o = { x: 1 };
		Object.defineProperty(o, "x", { get: function(){ return 42; } });
		function read(p){ return p.x; }
		read(o); read(o);   // run the site twice to exercise any caching
		read(o);
	`, "42")
}

// Many distinct shapes through one site (megamorphic-ish): the
// monomorphic cell just keeps refilling, but every read must be exact.
func TestICManyShapesReadStaysCorrect(t *testing.T) {
	evalEq(t, `
		function getk(o){ return o.k; }
		// each object puts k at a different slot via differing leading pads
		var objs = [
			{ k: 1 },
			{ a: 0, k: 2 },
			{ a: 0, b: 0, k: 3 },
			{ a: 0, b: 0, c: 0, k: 4 },
		];
		var sum = 0;
		for (var i = 0; i < 40; i++) sum += getk(objs[i % 4]);
		sum;   // 10 cycles * (1+2+3+4) = 100
	`, "100")
}
