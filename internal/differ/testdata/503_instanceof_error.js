let r;
try {
  let e = new TypeError("x");
  r = e instanceof TypeError;
} catch (err) {
  r = "fail";
}
r
