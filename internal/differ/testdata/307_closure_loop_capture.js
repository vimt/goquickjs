function makers() {
  let out = [];
  function makeOne(i) { return function() { return i; }; }
  for (let i = 0; i < 3; i = i + 1) {
    out.push(makeOne(i));
  }
  return out;
}
let fns = makers();
fns[0]() + fns[1]() + fns[2]()
