let a = null;
let b = "kept";
let c = 0;
a ||= "new";
b ||= "skip";
c ??= 99;
let d;
d ??= "set";
let e = 1;
let f = 0;
e &&= 5;
f &&= 5;
[a, b, c, d, e, f].join("|")
