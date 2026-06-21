// iterative fibonacci (mod P), repeated __N__ times. Tests tight scalar loops.
function main() {
  var reps = __N__;
  var total = 0;
  for (var r = 0; r < reps; r++) {
    var a = 0, b = 1;
    for (var i = 0; i < 90; i++) {
      var t = (a + b) % 1000000007;
      a = b;
      b = t;
    }
    total = (total + a) % 1000000007;
  }
  return total;
}
var result = "" + main();
result;
