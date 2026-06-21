let r = [];
for (let i = 0; i < 5; i = i + 1) {
  r.push(i % 2 === 0 ? "even" : "odd");
}
r.join(",")
