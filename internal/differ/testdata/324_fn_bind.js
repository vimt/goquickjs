function greet(prefix, name) { return prefix + ", " + name; }
let hi = greet.bind(undefined, "Hello");
hi("world")
