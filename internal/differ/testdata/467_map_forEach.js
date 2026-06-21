let m = new Map([["a", 1], ["b", 2], ["c", 3]]);
let r = [];
m.forEach(function (v, k) { r.push(k + "=" + v); });
r.join(",")
