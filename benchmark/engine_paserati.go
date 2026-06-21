//go:build !sizeprobe || eng_paserati

package main

import (
	"fmt"
	"strings"

	"github.com/nooga/paserati/pkg/driver"
)

func init() {
	register(Engine{
		Name:    "paserati",
		Lang:    "js",
		Version: moduleVersion("github.com/nooga/paserati"),
		Run: func(script string) (string, error) {
			p := driver.NewPaserati()
			defer p.Cleanup()
			// The benchmark suite is plain dynamic JS; paserati is a typed
			// (TypeScript-flavoured) engine, so skip type checking to avoid
			// spurious type errors on the untyped suite code.
			p.SetSkipTypeCheck(true)
			p.SetIgnoreTypeErrors(true)
			val, errs := p.RunString(script)
			if len(errs) > 0 {
				msgs := make([]string, 0, len(errs))
				for _, e := range errs {
					msgs = append(msgs, e.Error())
				}
				return "", fmt.Errorf("%s", strings.Join(msgs, "; "))
			}
			return val.ToString(), nil
		},
	})
}
