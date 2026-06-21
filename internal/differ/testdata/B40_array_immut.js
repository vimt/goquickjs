let a = [3, 1, 2];
let b = a.toReversed();
let c = a.toSorted();
let d = a.with(1, 99);
[a.join(","), b.join(","), c.join(","), d.join(",")].join("|")
