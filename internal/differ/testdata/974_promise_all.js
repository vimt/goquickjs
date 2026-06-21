let log = "";
Promise.all([Promise.resolve(1), Promise.resolve(2), 3])
  .then(arr => { log = arr.join(","); });
log
