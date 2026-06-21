[1, 2, 3, 4, 5]
  .filter(x => x % 2 === 1)
  .map(x => x * x)
  .reduce((a, b) => a + b, 0)
