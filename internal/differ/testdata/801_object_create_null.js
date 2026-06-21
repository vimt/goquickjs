let o = Object.create(null);
o.a = 1;
o.a + (Object.getPrototypeOf(o) === null ? 100 : 0)
