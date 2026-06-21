// Package jserrors hosts cross-layer sentinel errors so leaf packages
// (parser, compiler, vm) can produce errors the public API and the
// differential harness recognise without an import cycle through the
// root package.
package jserrors

import "errors"

// ErrNotImplemented marks features whose backing layer has not been
// built yet. The differ skips test cases whose SUT failure wraps this
// sentinel, so unfinished work shows up as SKIP rather than FAIL.
var ErrNotImplemented = errors.New("goquickjs: not implemented")
