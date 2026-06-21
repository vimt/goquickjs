function inner() { throw "from inner"; }
function outer() { inner(); return "not reached"; }
try {
  outer();
} catch (e) {
  "caught: " + e
}
