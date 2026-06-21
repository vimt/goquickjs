let e = new SyntaxError("bad");
[e instanceof SyntaxError, e instanceof Error, e.name, e.message]
