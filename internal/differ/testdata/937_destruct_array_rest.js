let arr = [1, 2, 3, 4, 5];
let [first, second, ...rest] = arr;
[first, second, rest.join(","), rest.length].join("|")
