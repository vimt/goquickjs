function makeCounter() {
  let n = 0;
  return function() { n = n + 1; return n; };
}
let c = makeCounter();
c() + c() + c()
