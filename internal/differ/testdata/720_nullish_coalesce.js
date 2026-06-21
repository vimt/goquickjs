let a = null ?? "default";
let b = undefined ?? "x";
let c = 0 ?? "kept-zero";
let d = "" ?? "kept-empty";
[a, b, c, d].join("/")
