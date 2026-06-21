let obj = {base: 100, hit: function(a, b) { return this.base + a + b; }};
let args = [3, 4];
obj.hit(...args)
