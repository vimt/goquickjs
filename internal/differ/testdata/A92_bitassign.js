let a = 0xff;
a &= 0x0f;
let b = 0x10;
b |= 0x01;
let c = 0xaa;
c ^= 0xff;
let d = 1;
d <<= 4;
let e = 256;
e >>= 2;
[a, b, c, d, e].join(",")
