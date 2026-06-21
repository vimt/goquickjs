[1, 2, 3, 4, 5]
  .filter(function(x) { return x % 2 === 1; })
  .map(function(x) { return x * x; })
  .reduce(function(a, b) { return a + b; }, 0)
