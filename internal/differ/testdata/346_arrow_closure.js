let counter = (function() {
  let n = 0;
  return () => ++n;
})();
counter() + counter() + counter()
