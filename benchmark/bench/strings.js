// build __N__ small strings, join them, then rolling-hash the char codes.
function main() {
  var N = __N__;
  var parts = [];
  for (var i = 0; i < N; i++) {
    parts.push("item" + (i % 1000));
  }
  var s = parts.join(",");
  var sum = 0;
  for (var i = 0; i < s.length; i++) {
    sum = (sum * 31 + s.charCodeAt(i)) % 1000000007;
  }
  return sum;
}
var result = "" + main();
result;
