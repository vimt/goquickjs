//go:build !sizeprobe || eng_goja

package main

import "github.com/dop251/goja"

func init() {
	register(Engine{
		Name:    "goja",
		Lang:    "js",
		Version: moduleVersion("github.com/dop251/goja"),
		Run: func(script string) (string, error) {
			vm := goja.New()
			v, err := vm.RunString(script)
			if err != nil {
				return "", err
			}
			return v.String(), nil
		},
	})
}
