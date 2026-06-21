let arr = [10, 20, 30];
let out = [];
for (let i in arr) out.push(i + ":" + arr[i]);
out.join("|")
