async function f() { return 42; }
let log = "init";
f().then(v => { log = "got:" + v; });
log
