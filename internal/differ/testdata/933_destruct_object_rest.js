let o = {a: 1, b: 2, c: 3, d: 4};
let {a, b, ...rest} = o;
[a, b, rest.c, rest.d, Object.keys(rest).join("|")].join(",")
