class A {}
class B extends A {}
let b = new B();
[b instanceof B, b instanceof A].join(",")
