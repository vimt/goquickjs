let arr = [1, 2, 3, 4, 5, 4, 3];
let last = arr.findLast(x => x === 4);
let lastIdx = arr.findLastIndex(x => x === 4);
[last, lastIdx, arr.findLast(x => x > 100)].join(",")
