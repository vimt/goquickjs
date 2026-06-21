//go:build !sizeprobe || eng_otto

package main

import "github.com/robertkrimen/otto"

// otto is an old-school pure-Go ES5 interpreter. It reuses the JS scripts.
func init() {
	register(Engine{
		Name:    "otto",
		Lang:    "js",
		Version: moduleVersion("github.com/robertkrimen/otto"),
		Run: func(script string) (string, error) {
			vm := otto.New()
			v, err := vm.Run(script)
			if err != nil {
				return "", err
			}
			return v.String(), nil
		},
	})
}
