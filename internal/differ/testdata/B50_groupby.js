let nums = [1, 2, 3, 4, 5, 6];
let g = Object.groupBy(nums, x => x % 2 === 0 ? "even" : "odd");
[g.odd.join(","), g.even.join(",")].join("|")
