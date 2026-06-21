let s = new Set();
s.add(NaN);
s.add(NaN);
s.size === 1 && s.has(NaN)
