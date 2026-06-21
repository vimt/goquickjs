let a = {x: 1, y: 2};
let b = {z: 3, ...a, w: 4};
[b.x, b.y, b.z, b.w, Object.keys(b).join("|")].join(",")
