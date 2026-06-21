function makePair() {
  let val = 0;
  function get() { return val; }
  function set(v) { val = v; }
  set(42);
  return get();
}
makePair()
