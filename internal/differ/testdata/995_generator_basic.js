function* gen() { yield 1; yield 2; yield 3; }
let g = gen();
[g.next().value, g.next().value, g.next().value, g.next().done].join(",")
