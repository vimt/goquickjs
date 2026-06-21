//go:build !sizeprobe || eng_quickjs

package main

import (
	"fmt"

	"modernc.org/quickjs"
)

func init() {
	register(Engine{
		Name:    "quickjs",
		Lang:    "js",
		Version: moduleVersion("modernc.org/quickjs"),
		Run: func(script string) (string, error) {
			m, err := quickjs.NewVM()
			if err != nil {
				return "", err
			}
			defer m.Close()
			res, err := m.Eval(script, quickjs.EvalGlobal)
			if err != nil {
				return "", err
			}
			return fmt.Sprint(res), nil
		},
	})
}
