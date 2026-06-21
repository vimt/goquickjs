async function add(a, b) { return a + b; }
let log = "init";
add(2, 3).then(v => { log = `sum is ${v}`; });
log
