let s = new Set();
s.add("hi");
s.add("yo");
let r = [];
s.forEach(function (v) { r.push(v); });
r.join(",")
