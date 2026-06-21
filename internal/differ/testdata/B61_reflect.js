let o = {a: 1, b: 2};
let g = Reflect.get(o, "a");
Reflect.set(o, "c", 3);
let has = Reflect.has(o, "c");
let keys = Reflect.ownKeys(o);
let del = Reflect.deleteProperty(o, "a");
[g, has, keys.join("|"), del, Reflect.has(o, "a")].join(",")
