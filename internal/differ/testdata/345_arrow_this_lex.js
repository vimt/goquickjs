let obj = {
  vals: [1, 2, 3],
  scale: 10,
  total: function() {
    return this.vals.reduce((acc, v) => acc + v * this.scale, 0);
  }
};
obj.total()
