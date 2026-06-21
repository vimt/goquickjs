function* gen() {
  let a = yield "first";
  let b = yield a + "!";
  return a + "/" + b;
}
let g = gen();
let r1 = g.next();
let r2 = g.next("hello");
let r3 = g.next("done");
[r1.value, r2.value, r3.value, r3.done].join("|")
