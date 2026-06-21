// nested integer loops + modular arithmetic. __N__ is injected by the host.
function main() {
  var N = __N__;
  var acc = 0;
  for (var i = 0; i < N; i++) {
    for (var j = 0; j < 100; j++) {
      acc = (acc + i * 31 + j * 7) % 1000000007;
    }
  }
  return acc;
}
var result = "" + main();
result;
