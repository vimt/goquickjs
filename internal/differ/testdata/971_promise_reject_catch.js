let log = "";
Promise.reject("bad").catch(e => { log = "caught:" + e; });
log
