let re = new RegExp("foo", "i");
[re.test("FoO"), re.source, re.flags].join("/")
