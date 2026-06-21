function f(a, b = 10, c = a + b) { return a + ":" + b + ":" + c; }
[f(1), f(1, 2), f(1, 2, 3)].join("|")
