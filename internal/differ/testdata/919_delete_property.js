let o = {x: 1, y: 2, z: 3};
let r1 = delete o.x;
let key = "y";
let r2 = delete o[key];
[r1, r2, o.x, o.y, o.z, Object.keys(o).join("|")].join(",")
