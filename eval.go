package goquickjs

import (
	"github.com/vimt/goquickjs/internal/builtins"
	"github.com/vimt/goquickjs/internal/compiler"
	"github.com/vimt/goquickjs/internal/jserrors"
	"github.com/vimt/goquickjs/internal/parser"
	"github.com/vimt/goquickjs/internal/value"
	"github.com/vimt/goquickjs/internal/vm"
)

// ErrNotImplemented marks APIs whose backing layer has not been built
// yet. Re-exported from internal/jserrors so leaf packages can wrap
// the same sentinel without importing this package (cycle-free).
var ErrNotImplemented = jserrors.ErrNotImplemented

// Eval parses, compiles, and runs a JavaScript source string in a
// fresh VM, returning the completion value rendered as a canonical
// string suitable for differential comparison against the QuickJS
// oracle.
func Eval(src string) (string, error) {
	prog, err := parser.Parse(src)
	if err != nil {
		return "", err
	}
	chunk, err := compiler.Compile(prog)
	if err != nil {
		return "", err
	}
	v, err := vm.Run(chunk, defaultGlobals())
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

// defaultGlobals constructs a fresh globals map for each Eval call.
// Built-ins (Math, Array, String, ...) are installed via the
// builtins package; mutations users make to those objects don't
// leak across Evals.
func defaultGlobals() map[string]value.Value {
	g := map[string]value.Value{}
	builtins.Install(g)
	return g
}
