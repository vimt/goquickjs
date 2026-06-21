//go:build !sizeprobe || eng_tengo

package main

import (
	"fmt"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"
)

// tengo is a popular pure-Go scripting language (bytecode VM, Go-ish syntax).
func init() {
	register(Engine{
		Name:    "tengo",
		Lang:    "tengo",
		Version: moduleVersion("github.com/d5/tengo/v2"),
		Run: func(script string) (string, error) {
			s := tengo.NewScript([]byte(script))
			s.SetImports(stdlib.GetModuleMap("text", "fmt"))
			compiled, err := s.Run()
			if err != nil {
				return "", err
			}
			v := compiled.Get("result")
			if v == nil {
				return "", fmt.Errorf("script did not set 'result'")
			}
			return v.String(), nil
		},
	})
}
