//go:build !sizeprobe || eng_gopherlua

package main

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

func init() {
	register(Engine{
		Name:    "gopher-lua",
		Lang:    "lua",
		Version: moduleVersion("github.com/yuin/gopher-lua"),
		Run: func(script string) (string, error) {
			// Raise the data-stack registry and let it grow: table.concat pushes
			// every element onto the stack, which overflows the default 5120-slot
			// registry on the larger benchmarks.
			L := lua.NewState(lua.Options{
				RegistrySize:    1 << 16,
				RegistryMaxSize: 1 << 23,
				CallStackSize:   1 << 12,
			})
			defer L.Close()
			if err := L.DoString(script); err != nil {
				return "", err
			}
			v := L.GetGlobal("result")
			if v == lua.LNil {
				return "", fmt.Errorf("script did not set global 'result'")
			}
			return v.String(), nil
		},
	})
}
