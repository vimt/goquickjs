let o = {a: 1, b: 2, c: 3};
let out = [];
for (let k in o) out.push(k + "=" + o[k]);
out.join(",")
