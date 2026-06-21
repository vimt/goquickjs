function Greeter() {}
Greeter.prototype.hi = function(name) { return "Hi " + name; };
let a = new Greeter();
let b = new Greeter();
a.hi("alice") + " / " + b.hi("bob")
