let parent = {x: 1};
let child = Object.create(parent);
[parent.isPrototypeOf(child), child.isPrototypeOf(parent)].join(",")
