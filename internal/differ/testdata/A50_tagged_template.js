function tag(strs, ...vals) {
  let out = "";
  for (let i = 0; i < strs.length; i++) {
    out += "[" + strs[i] + "]";
    if (i < vals.length) out += "(" + vals[i] + ")";
  }
  return out;
}
let a = 1, b = 2;
tag`x=${a}, y=${b}; done`
