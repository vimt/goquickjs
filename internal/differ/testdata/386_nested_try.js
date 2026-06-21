try {
  try {
    throw "inner";
  } catch (e) {
    throw "wrapped:" + e;
  }
} catch (e) {
  e
}
