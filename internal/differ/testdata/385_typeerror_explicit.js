function bad() { throw new TypeError("nope"); }
let caught;
try { bad(); } catch (e) { caught = e.name + ": " + e.message; }
caught
