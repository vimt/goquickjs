async function f() {
  let p = new Promise(r => Promise.resolve().then(() => r("inner")));
  let v = await p;
  return v + "!";
}
let log = "init";
f().then(v => { log = v; });
log
