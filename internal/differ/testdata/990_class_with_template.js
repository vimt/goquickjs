class Greeter {
  constructor(name) { this.name = name; }
  hello() { return `Hello, ${this.name}!`; }
}
new Greeter("world").hello()
