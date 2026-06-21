let log = "";
try { log += "try;"; } finally { log += "fin;"; }
try { log += "try2;"; throw "x"; } catch (e) { log += "caught:" + e + ";"; } finally { log += "fin2;"; }
log
