let ok = "fine";
try {
  ok = "still fine";
} catch (e) {
  ok = "should not happen";
}
ok
