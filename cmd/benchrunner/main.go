// Quick & dirty bench-script runner. Reads a JS bench from the
// go-script project (replacing __N__ with a chosen size), runs it
// through both goquickjs and modernc.org/quickjs, prints both
// results so we can eyeball agreement.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/vimt/goquickjs"
	"modernc.org/quickjs"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: benchrunner <bench.js> [N]")
		os.Exit(2)
	}
	path := os.Args[1]
	n := "100"
	if len(os.Args) >= 3 {
		n = os.Args[2]
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	src := strings.ReplaceAll(string(raw), "__N__", n)

	// Upstream oracle.
	var oracleOut string
	vm, _ := quickjs.NewVM()
	res, err := vm.Eval(src, quickjs.EvalGlobal)
	if err != nil {
		oracleOut = "ERR: " + err.Error()
	} else {
		oracleOut = fmt.Sprint(res)
	}
	vm.Close()

	// Our SUT.
	var sutOut string
	if r, err := goquickjs.Eval(src); err != nil {
		sutOut = "ERR: " + err.Error()
	} else {
		sutOut = r
	}

	fmt.Printf("file:   %s  (N=%s)\n", path, n)
	fmt.Printf("oracle: %s\n", oracleOut)
	fmt.Printf("sut:    %s\n", sutOut)
	if oracleOut != sutOut {
		fmt.Println("DIVERGE")
		os.Exit(1)
	}
	fmt.Println("OK")
}
