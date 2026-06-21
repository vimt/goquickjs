//go:build !sizeprobe || eng_shopifylua

package main

import (
	"fmt"

	golua "github.com/Shopify/go-lua"
)

// Shopify/go-lua is a second pure-Go Lua implementation (Lua 5.2). It reuses the
// .lua scripts, for a head-to-head against gopher-lua.
func init() {
	register(Engine{
		Name:    "shopify-lua",
		Lang:    "lua",
		Version: moduleVersion("github.com/Shopify/go-lua"),
		Run: func(script string) (string, error) {
			l := golua.NewState()
			golua.OpenLibraries(l)
			if err := golua.DoString(l, script); err != nil {
				return "", err
			}
			l.Global("result")
			s, ok := l.ToString(-1)
			if !ok {
				return "", fmt.Errorf("global 'result' is not a string")
			}
			return s, nil
		},
	})
}
