function outer(a) {
  return function(b) {
    return function(c) {
      return a + b + c;
    };
  };
}
outer(100)(20)(3)
