async function f() {
  let v = await 99;
  return v;
}
let log = "init";
f().then(v => { log = "" + v; });
log
