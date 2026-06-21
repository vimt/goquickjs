function Coerce() {
  this.x = 1;
  return {x: 99};
}
let c = new Coerce();
c.x
