let counter = 0;
let r1 = void (counter = counter + 1);
let r2 = void counter;
[r1, r2, counter].join(",")
