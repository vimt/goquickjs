function pick({a, b}) { return a + ":" + b; }
function head([first, second]) { return first + "/" + second; }
[pick({a: 1, b: 2, c: 99}), head([10, 20, 30])].join("|")
