let log = "";
new Promise((resolve, reject) => {
  resolve("ok");
}).then(v => { log = v; });
log
