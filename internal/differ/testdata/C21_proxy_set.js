let log = [];
let t = {a: 1};
let p = new Proxy(t, {
  set: function(target, k, v) {
    log.push(k + "=" + v);
    target[k] = v * 100;
    return true;
  }
});
p.a = 5;
p.b = 7;
[t.a, t.b, log.join("|")].join(",")
