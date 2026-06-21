let s = new Set([1, 2, 3, 2, 1]);
let total = 0;
for (let v of s.values()) total = total + v;
total
