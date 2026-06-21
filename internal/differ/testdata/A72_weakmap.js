let m = new WeakMap();
let k1 = {id: 1};
let k2 = {id: 2};
m.set(k1, "a");
m.set(k2, "b");
[m.get(k1), m.get(k2), m.has(k1), m.has({}), m.delete(k1), m.has(k1)].join(",")
