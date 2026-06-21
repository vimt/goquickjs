let caught;
try {
  for (let x of 42) caught = "no throw";
} catch (e) {
  caught = e.name;
}
caught
