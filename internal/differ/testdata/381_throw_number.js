let caught = 0;
try {
  throw 42;
} catch (n) {
  caught = n;
}
caught
