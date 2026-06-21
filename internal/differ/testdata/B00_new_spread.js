class Point {
  constructor(x, y, z) { this.s = x + ":" + y + ":" + z; }
}
let args = [1, 2, 3];
let p = new Point(...args);
let q = new Point(0, ...[5, 6]);
[p.s, q.s].join("|")
