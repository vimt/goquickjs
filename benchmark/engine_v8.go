//go:build v8

// V8 (the real Google engine) via the tommie/v8go cgo binding. It pulls in a
// large prebuilt static libv8, so it is gated behind the `v8` build tag:
//
//	go run -tags v8 .
package main

import v8 "github.com/tommie/v8go"

func init() {
	register(Engine{
		Name:    "v8",
		Lang:    "js",
		Version: moduleVersion("github.com/tommie/v8go"),
		Run: func(script string) (string, error) {
			iso := v8.NewIsolate()
			defer iso.Dispose()
			ctx := v8.NewContext(iso)
			defer ctx.Close()
			val, err := ctx.RunScript(script, "bench.js")
			if err != nil {
				return "", err
			}
			return val.String(), nil
		},
	})
}
