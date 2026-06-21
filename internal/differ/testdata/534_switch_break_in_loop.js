let xs = [1, 2, 3, 4, 5];
let r = [];
for (let i = 0; i < xs.length; i = i + 1) {
  switch (xs[i] % 3) {
    case 0:
      r.push("z");
      break;
    case 1:
      r.push("o");
      break;
    default:
      r.push("t");
  }
}
r.join(",")
