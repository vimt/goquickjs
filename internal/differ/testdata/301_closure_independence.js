function makeCounter() {
  let n = 0;
  return function() { n = n + 1; return n; };
}
let a = makeCounter();
let b = makeCounter();
a(); a(); a();
b()
