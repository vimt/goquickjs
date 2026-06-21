let o = Object.create(null);
o.a = 5;
[Object.getPrototypeOf(o) === null, o.a].join(",")
