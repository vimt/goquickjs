// build an array via a MINSTD LCG, insertion-sort it, then rolling-hash the
// sorted values (order matters, index base does not). __N__ = array size.
function main() {
  var S = __N__;
  var seed = 12345;
  var arr = [];
  for (var i = 0; i < S; i++) {
    seed = (seed * 48271) % 2147483647;
    arr.push(seed % 10000);
  }
  for (var i = 1; i < S; i++) {
    var key = arr[i];
    var j = i - 1;
    for (var k = 0; k < i; k++) {
      if (j >= 0 && arr[j] > key) {
        arr[j + 1] = arr[j];
        j--;
      } else {
        break;
      }
    }
    arr[j + 1] = key;
  }
  var sum = 0;
  for (var i = 0; i < S; i++) {
    sum = (sum * 31 + arr[i]) % 1000000007;
  }
  return sum;
}
var result = "" + main();
result;
