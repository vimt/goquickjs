let iterable = {
  [Symbol.iterator]: function() {
    let i = 0;
    return {
      next: function() {
        if (i < 3) return {value: i++, done: false};
        return {value: undefined, done: true};
      }
    };
  }
};
let out = [];
for (let v of iterable) out.push(v);
out.join(",")
