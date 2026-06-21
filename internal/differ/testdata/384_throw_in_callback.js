let result;
try {
  [1, 2, 3].map(function(x) {
    if (x === 2) throw "stop";
    return x * 10;
  });
  result = "no throw";
} catch (e) {
  result = "got " + e;
}
result
