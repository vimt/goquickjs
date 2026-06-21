async function f() {
  let x = await Promise.resolve(7);
  return x + 1;
}
let log = "init";
f().then(v => { log = "got:" + v; });
log
