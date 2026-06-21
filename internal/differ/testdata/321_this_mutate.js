let obj = {
  n: 0,
  inc: function() { this.n = this.n + 1; return this.n; }
};
obj.inc();
obj.inc();
obj.inc()
