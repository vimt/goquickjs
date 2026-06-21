function* gen() { yield "a"; yield "b"; yield "c"; }
let out = [];
for (let x of gen()) out.push(x);
out.join(",")
