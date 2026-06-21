class Animal {
  constructor(name) { this.name = name; }
  describe() { return "An animal called " + this.name; }
}
class Dog extends Animal {
  bark() { return this.name + " says woof"; }
}
let d = new Dog("rex");
[d.describe(), d.bark()].join(" / ")
