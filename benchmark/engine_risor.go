//go:build !sizeprobe || eng_risor

package main

import (
	"context"

	"github.com/risor-io/risor"
)

// risor is a newer pure-Go scripting language (Go/Python-flavoured). The final
// expression is the program's value; Inspect() renders it.
func init() {
	register(Engine{
		Name:    "risor",
		Lang:    "risor",
		Version: moduleVersion("github.com/risor-io/risor"),
		Run: func(script string) (string, error) {
			v, err := risor.Eval(context.Background(), script)
			if err != nil {
				return "", err
			}
			return v.Inspect(), nil
		},
	})
}
