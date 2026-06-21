let s = new WeakSet();
let a = {x: 1};
let b = {x: 2};
s.add(a);
s.add(b);
[s.has(a), s.has(b), s.has({}), s.delete(a), s.has(a)].join(",")
