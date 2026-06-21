function A() { this.kind = "A"; }
A.prototype.who = function() { return this.kind; };
let a = new A();
a.who()
