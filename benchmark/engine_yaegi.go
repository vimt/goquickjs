//go:build !sizeprobe || eng_yaegi

package main

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// yaegi runs Go itself as a scripting language. The scripts are Go (.ygo so the
// Go toolchain doesn't try to compile them as package files); the final
// expression's value is returned via reflect.
func init() {
	register(Engine{
		Name:    "yaegi",
		Lang:    "go",
		Version: moduleVersion("github.com/traefik/yaegi"),
		Run: func(script string) (string, error) {
			i := interp.New(interp.Options{})
			if err := i.Use(stdlib.Symbols); err != nil {
				return "", err
			}
			v, err := i.Eval(script)
			if err != nil {
				return "", err
			}
			if !v.IsValid() {
				return "", fmt.Errorf("nil result")
			}
			// Unwrap any pointer/interface yaegi may wrap the result in.
			for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
				if v.IsNil() {
					return "", fmt.Errorf("nil result")
				}
				v = v.Elem()
			}
			switch v.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				return strconv.FormatInt(v.Int(), 10), nil
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				return strconv.FormatUint(v.Uint(), 10), nil
			}
			return fmt.Sprintf("%v", v.Interface()), nil
		},
	})
}
