function Base() {}
function Sub() {}
Sub.prototype = new Base();
let s = new Sub();
[s instanceof Sub, s instanceof Base]
