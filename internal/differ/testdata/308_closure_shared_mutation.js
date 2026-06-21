function makeAccumulator() {
  let total = 0;
  function add(n) { total = total + n; return total; }
  function reset() { total = 0; return total; }
  add(10); add(20); add(30);
  reset();
  return add(5);
}
makeAccumulator()
