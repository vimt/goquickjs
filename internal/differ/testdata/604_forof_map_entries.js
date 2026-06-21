let m = new Map([["a", 1], ["b", 2], ["c", 3]]);
let r = [];
for (let e of m.entries()) r.push(e[0] + "=" + e[1]);
r.join(",")
