function label(n) {
  switch (n) {
    case 1: return "one";
    case 2: return "two";
    case 3: return "three";
    default: return "other";
  }
}
label(1) + "/" + label(2) + "/" + label(99)
