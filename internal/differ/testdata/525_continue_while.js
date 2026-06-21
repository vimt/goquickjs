let sum = 0;
let i = 0;
while (i < 10) {
  i = i + 1;
  if (i % 3 === 0) continue;
  sum = sum + i;
}
sum
