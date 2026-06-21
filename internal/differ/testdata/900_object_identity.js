let a = {x: 1};
let b = a;
let c = {x: 1};
let arr = [1, 2];
let arr2 = arr;
[a === b, a === c, a !== c, arr === arr2, arr === [1, 2]].join(",")
