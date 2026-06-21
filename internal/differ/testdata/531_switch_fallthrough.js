function classify(n) {
  let r = "";
  switch (n) {
    case 1:
      r = r + "1";
    case 2:
      r = r + "2";
      break;
    case 3:
      r = r + "3";
  }
  return r;
}
classify(1) + "/" + classify(2) + "/" + classify(3)
