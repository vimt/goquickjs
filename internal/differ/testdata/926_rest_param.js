function tail(first, ...rest) {
  return first + ":" + rest.join(",");
}
[tail("a"), tail("a", "b"), tail("a", "b", "c", "d")].join("|")
