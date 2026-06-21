function f() {
  let out = [];
  for (let i = 0; i < arguments.length; i++) out.push(arguments[i]);
  return out.join(",");
}
[f(), f(1), f(1, 2, 3), f("a", "b")].join("|")
