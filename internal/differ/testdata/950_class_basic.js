class Point {
  constructor(x, y) { this.x = x; this.y = y; }
  sum() { return this.x + this.y; }
}
let p = new Point(3, 4);
[p.x, p.y, p.sum()].join(",")
