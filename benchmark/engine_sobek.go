//go:build !sizeprobe || eng_sobek

package main

import "github.com/grafana/sobek"

func init() {
	register(Engine{
		Name:    "sobek",
		Lang:    "js",
		Version: moduleVersion("github.com/grafana/sobek"),
		Run: func(script string) (string, error) {
			vm := sobek.New()
			v, err := vm.RunString(script)
			if err != nil {
				return "", err
			}
			return v.String(), nil
		},
	})
}
