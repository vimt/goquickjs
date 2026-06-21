let obj = {a: {b: 42}};
let r1 = obj?.a?.b;
let r2 = obj?.c?.d;
let r3 = obj?.a?.missing;
[r1, r2, r3]
