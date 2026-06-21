let getter = function() { return this.x; };
let bound = getter.bind({x: 7});
bound()
