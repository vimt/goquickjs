let o = {
  _x: 1,
  get x() { return this._x; },
  set x(v) { this._x = v * 2; }
};
let r1 = o.x;
o.x = 10;
let r2 = o.x;
[r1, r2].join(",")
