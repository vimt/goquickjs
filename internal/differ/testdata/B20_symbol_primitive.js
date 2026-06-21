let s = Symbol("x");
let t = Symbol("x");
let u = Symbol.for("y");
let v = Symbol.for("y");
[typeof s, s === t, u === v, s.description, Symbol.keyFor(u)].join("|")
