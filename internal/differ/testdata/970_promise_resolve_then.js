let log = "";
Promise.resolve(42).then(v => { log = "got:" + v; });
log
