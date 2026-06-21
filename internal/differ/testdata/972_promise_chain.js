let log = "";
Promise.resolve(1)
  .then(v => v + 10)
  .then(v => v * 2)
  .then(v => { log = "final:" + v; });
log
