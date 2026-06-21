let r = [];
for (let x of [1, 2, 3, 4, 5]) {
  if (x % 2 === 0) continue;
  r.push(x);
}
r
