async function step(x) { return x + 1; }
async function chain() {
  let a = await step(0);
  let b = await step(a);
  let c = await step(b);
  return c;
}
let log = "init";
chain().then(v => { log = "got:" + v; });
log
