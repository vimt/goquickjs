let s = new Set([1, 2, 3]);
let removed = s.delete(2);
removed === true && s.has(2) === false && s.size === 2
