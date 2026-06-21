let o = {_n: 5};
Object.defineProperty(o, "n", {
  get: function() { return this._n * 10; },
  set: function(v) { this._n = v; }
});
let r1 = o.n;
o.n = 7;
let r2 = o.n;
[r1, r2, o._n].join(",")
