let o = {
  base: 10,
  add(x) { return this.base + x; },
  mul(x) { return this.base * x; }
};
[o.add(5), o.mul(3)].join(",")
