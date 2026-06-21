function Box(v) { this.v = v; }
Box.prototype.get = function() { return this.v; };
new Box(42).get()
