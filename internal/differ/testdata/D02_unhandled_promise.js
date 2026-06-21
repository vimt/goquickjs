let log = "init";
Promise.reject("oops").catch(e => { log = "caught:" + e; });
log
