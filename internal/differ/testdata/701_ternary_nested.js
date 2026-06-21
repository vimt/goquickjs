function classify(n) {
  return n > 0 ? "pos" : n < 0 ? "neg" : "zero";
}
[classify(5), classify(-3), classify(0)].join(",")
