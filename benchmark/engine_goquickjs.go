//go:build !sizeprobe || eng_goquickjs

package main

import (
	"github.com/vimt/goquickjs"
)

// goquickjs is the in-tree pure-Go JS engine we're building from scratch
// (see ./goquickjs). Wired into the same Engine registry as every other
// runtime so it competes head-to-head on the same bench scripts.
func init() {
	register(Engine{
		Name:    "goquickjs",
		Lang:    "js",
		Version: "0.0.0-dev",
		Run: func(script string) (string, error) {
			return goquickjs.Eval(script)
		},
	})
}
