function* range(n) {
  for (let i = 0; i < n; i++) yield i;
}
let out = [];
let r = range(5);
let v = r.next();
while (!v.done) {
  out.push(v.value);
  v = r.next();
}
out.join(",")
