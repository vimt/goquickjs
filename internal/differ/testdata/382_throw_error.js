try {
  throw new Error("boom");
} catch (e) {
  e.message
}
