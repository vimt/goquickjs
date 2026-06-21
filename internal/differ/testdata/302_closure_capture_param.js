function adder(x) {
  return function(y) { return x + y; };
}
let add10 = adder(10);
add10(5) + add10(7)
