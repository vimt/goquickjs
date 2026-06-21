let key = "dynamic";
let n = 42;
let o = {[key]: "v1", ["num_" + n]: 100, fixed: "f"};
[o.dynamic, o.num_42, o.fixed].join(",")
