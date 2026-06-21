let f = (x = 5, y = 10) => x * y;
[f(), f(2), f(2, 3)].join(",")
