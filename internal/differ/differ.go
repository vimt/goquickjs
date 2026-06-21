// Package differ runs the same JavaScript snippet through two engines —
// an oracle and a system-under-test — and reports any divergence.
//
// The oracle is modernc.org/quickjs (the upstream QuickJS we are
// reimplementing). The SUT is goquickjs itself. Until the SUT grows
// real evaluation it returns ErrNotImplemented, which the harness
// treats as a skip rather than a divergence so the oracle's expected
// output is still recorded.
package differ

import (
	"errors"
	"fmt"

	"modernc.org/quickjs"

	"github.com/vimt/goquickjs"
)

// Evaluator renders a JS source string into a canonical text result.
type Evaluator interface {
	Name() string
	Eval(src string) (string, error)
}

// Side captures one engine's outcome for a given source.
type Side struct {
	Value string
	Err   error
}

// Diff is the side-by-side outcome of running the same source through
// two evaluators.
type Diff struct {
	Source string
	Oracle Side
	SUT    Side
}

// Equal reports whether oracle and SUT agree.
//
// We treat "both errored" as agreement without comparing messages —
// different engines word their errors differently and our SUT will
// never reproduce QuickJS's exact phrasing. Once the SUT can classify
// errors (SyntaxError vs TypeError vs RangeError) we'll tighten this.
func (d Diff) Equal() bool {
	if (d.Oracle.Err == nil) != (d.SUT.Err == nil) {
		return false
	}
	if d.Oracle.Err != nil {
		return true
	}
	return d.Oracle.Value == d.SUT.Value
}

// SkipNYI reports whether the SUT failed because its layer is not yet
// built. Callers use this to skip — not fail — corpus entries that
// outrun the current implementation.
func (d Diff) SkipNYI() bool {
	return errors.Is(d.SUT.Err, goquickjs.ErrNotImplemented)
}

// Run feeds src through both engines and returns the comparison.
func Run(oracle, sut Evaluator, src string) Diff {
	return Diff{
		Source: src,
		Oracle: evalOne(oracle, src),
		SUT:    evalOne(sut, src),
	}
}

func evalOne(e Evaluator, src string) Side {
	v, err := e.Eval(src)
	return Side{Value: v, Err: err}
}

// QuickJSOracle binds modernc.org/quickjs as the reference engine.
// A fresh VM per call keeps tests order-independent.
type QuickJSOracle struct{}

func (QuickJSOracle) Name() string { return "modernc.org/quickjs" }

func (QuickJSOracle) Eval(src string) (string, error) {
	vm, err := quickjs.NewVM()
	if err != nil {
		return "", err
	}
	defer vm.Close()
	res, err := vm.Eval(src, quickjs.EvalGlobal)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(res), nil
}

// GoQuickJSSUT routes through the public goquickjs.Eval entry point so
// the harness exercises the same surface real users will.
type GoQuickJSSUT struct{}

func (GoQuickJSSUT) Name() string { return "github.com/vimt/goquickjs" }

func (GoQuickJSSUT) Eval(src string) (string, error) {
	return goquickjs.Eval(src)
}
