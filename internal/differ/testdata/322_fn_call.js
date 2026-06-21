function get() { return this.v; }
let obj = {v: 99};
get.call(obj)
