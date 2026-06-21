let r = 0;
for (let i = 0; i < 10; i = i + 1) {
  if (i % 2 === 0) continue;
  r = r + i;
}
r
