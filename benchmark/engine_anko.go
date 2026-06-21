//go:build !sizeprobe || eng_anko

package main

import (
	"fmt"

	"github.com/mattn/anko/core"
	"github.com/mattn/anko/env"
	"github.com/mattn/anko/vm"
)

// anko is a pure-Go interpreter with Go-like syntax. The script's last
// expression is its return value; we render it as a decimal string.
func init() {
	register(Engine{
		Name:    "anko",
		Lang:    "anko",
		Version: moduleVersion("github.com/mattn/anko"),
		Run: func(script string) (string, error) {
			e := env.NewEnv()
			core.Import(e)
			v, err := vm.Execute(e, nil, script)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%v", v), nil
		},
	})
}
