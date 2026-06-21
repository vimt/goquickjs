let o = {a: {x: 1, y: 2}, b: [10, 20]};
let {a: {x, y}, b: [p, q]} = o;
[x, y, p, q].join(",")
