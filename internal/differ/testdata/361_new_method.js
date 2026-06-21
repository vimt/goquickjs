function Counter(start) {
  this.n = start;
}
Counter.prototype.bump = function() { this.n = this.n + 1; return this.n; };
let c = new Counter(10);
c.bump(); c.bump(); c.bump()
