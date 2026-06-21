let result = (function(seed) {
  return function() { return seed * seed; };
})(7)();
result
