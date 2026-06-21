let log = [];
let p = new Proxy({a: 1, b: 2}, {
  get: function(t, k) {
    log.push("get:" + k);
    return t[k] * 10;
  }
});
let v1 = p.a;
let v2 = p.b;
[v1, v2, log.join(",")].join("|")
