function day(n) {
  switch (n) {
    case 0: return "Sun";
    case 6: return "Sat";
    default: return "Weekday";
  }
}
[day(0), day(3), day(6), day(99)]
