let f = (...xs) => xs.reduce((a, b) => a + b, 0);
[f(), f(1), f(1,2,3,4,5)].join(",")
