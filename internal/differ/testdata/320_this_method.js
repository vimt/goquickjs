let obj = {
  v: 42,
  get: function() { return this.v; }
};
obj.get()
