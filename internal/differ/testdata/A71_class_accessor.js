class Box {
  constructor(v) { this._v = v; }
  get value() { return this._v; }
  set value(v) { this._v = v + 100; }
}
let b = new Box(5);
let r1 = b.value;
b.value = 1;
let r2 = b.value;
[r1, r2].join(",")
