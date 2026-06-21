package goquickjs

import "testing"

// propHeavy stresses plain-object own-property reads in a hot loop: the
// exact pattern monomorphic GetProp inline caching targets. Every object
// shares one Shape (built via the same {x,y,z} path), so a per-site IC
// should turn each `p.x` from a map lookup into a pointer-compare.
const propHeavy = `
function main() {
  var pts = [];
  for (var i = 0; i < 2000; i++) pts.push({ x: i, y: i + 1, z: i + 2 });
  var sum = 0;
  for (var iter = 0; iter < 200; iter++) {
    for (var i = 0; i < pts.length; i++) {
      var p = pts[i];
      sum = (sum + p.x + p.y + p.z) % 1000000007;
    }
  }
  return sum;
}
main();
`

func BenchmarkPropAccess(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := Eval(propHeavy); err != nil {
			b.Fatal(err)
		}
	}
}

// propDominated isolates GetProp: one fixed object, a tight loop that
// does almost nothing but read its fields. The IC fast path should
// dominate the measurement here.
const propDominated = `
function main() {
  var o = { a: 1, b: 2, c: 3, d: 4 };
  var sum = 0;
  for (var i = 0; i < 2000000; i++) {
    sum = (sum + o.a + o.b + o.c + o.d) % 1000000007;
  }
  return sum;
}
main();
`

func BenchmarkPropDominated(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := Eval(propDominated); err != nil {
			b.Fatal(err)
		}
	}
}

// writeHeavy stresses OpSetProp: the update regime (re-writing existing
// fields in a hot loop) plus the add regime (each {a,b,c,d} literal
// builds the same shape via the same transitions).
const writeHeavy = `
function main() {
  var sum = 0;
  for (var i = 0; i < 400000; i++) {
    var o = { a: 0, b: 0, c: 0, d: 0 }; // add regime: 4 transitions
    o.a = i; o.b = i + 1; o.c = i + 2; o.d = i + 3; // update regime
    sum = (sum + o.a + o.b + o.c + o.d) % 1000000007;
  }
  return sum;
}
main();
`

func BenchmarkPropWrite(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := Eval(writeHeavy); err != nil {
			b.Fatal(err)
		}
	}
}
