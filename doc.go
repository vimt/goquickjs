// Package goquickjs is a pure-Go reimplementation of QuickJS, in progress.
//
// The codebase is being built up in layers: lexer/parser → bytecode →
// VM → builtins → regex/unicode. Public APIs return ErrNotImplemented
// until the relevant layer lands.
package goquickjs
