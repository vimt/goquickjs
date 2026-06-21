let proto = {a: 1};
let o = Object.create(proto);
// inherited a is visible via o.a but not as an own prop
[o.a, Object.hasOwn(o, "a")].join(",")
