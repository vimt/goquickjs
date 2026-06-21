let o = {x: 1};
let {x, y = 99, z = "fallback"} = o;
[x, y, z].join(",")
