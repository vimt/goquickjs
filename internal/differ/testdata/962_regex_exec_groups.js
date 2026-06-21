let re = /(\d+)-(\d+)/;
let m = re.exec("phone 415-1234 ext");
[m[0], m[1], m[2]].join("|")
