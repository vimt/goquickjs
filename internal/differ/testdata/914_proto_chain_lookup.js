let parent = {greet: function() { return "hi"; }};
let child = Object.create(parent);
child.greet()
