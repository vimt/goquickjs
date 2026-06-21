//go:build !sizeprobe || eng_starlark

package main

import (
	"fmt"

	"go.starlark.net/starlark"
)

func init() {
	register(Engine{
		Name:    "starlark",
		Lang:    "starlark",
		Version: moduleVersion("go.starlark.net"),
		Run: func(script string) (string, error) {
			thread := &starlark.Thread{Name: "bench"}
			globals, err := starlark.ExecFile(thread, "bench.star", script, nil)
			if err != nil {
				return "", err
			}
			v, ok := globals["result"]
			if !ok {
				return "", fmt.Errorf("script did not set global 'result'")
			}
			if s, ok := starlark.AsString(v); ok {
				return s, nil
			}
			return v.String(), nil
		},
	})
}
