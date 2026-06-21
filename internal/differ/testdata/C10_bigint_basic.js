let a = 12345678901234567890n;
let b = 2n;
let c = a + b;
let d = a * 3n;
let e = BigInt(100);
[typeof a, a + "", c + "", d + "", e + "", a === 12345678901234567890n].join(",")
