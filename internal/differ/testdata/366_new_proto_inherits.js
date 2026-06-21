function Base() {}
Base.prototype.greet = function() { return "base"; };
function Sub() {}
Sub.prototype = new Base();
let s = new Sub();
s.greet()
