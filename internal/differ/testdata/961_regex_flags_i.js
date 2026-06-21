let re = /WORLD/i;
[re.test("hello world"), re.test("hello WORLD"), re.test("nope")].join(",")
