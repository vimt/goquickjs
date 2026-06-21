async function step(x) {
  let p = new Promise(r => Promise.resolve().then(() => r(x + 1)));
  return await p;
}
async function chain() {
  let a = await step(0);
  let b = await step(a);
  let c = await step(b);
  return c;
}
let log = "init";
chain().then(v => { log = "got:" + v; });
log
