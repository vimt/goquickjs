module github.com/vimt/goquickjs/benchmark

go 1.26.3

require (
	github.com/Shopify/go-lua v0.0.0-20250718183320-1e37f32ad7d0
	github.com/d5/tengo/v2 v2.17.0
	github.com/dop251/goja v0.0.0-20260607120635-348e6bea910d
	github.com/grafana/sobek v0.0.0-20260612080906-524cb275218c
	github.com/mattn/anko v0.1.12
	github.com/nooga/paserati v0.9.12
	github.com/risor-io/risor v1.8.1
	github.com/robertkrimen/otto v0.5.1
	github.com/tommie/v8go v0.34.0
	github.com/traefik/yaegi v0.16.1
	github.com/vimt/goquickjs v0.0.0
	github.com/yuin/gopher-lua v1.1.2
	go.starlark.net v0.0.0-20260613233743-8ba36ccb83fb
	modernc.org/quickjs v0.18.2
)

require (
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/dlclark/regexp2/v2 v2.2.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/tommie/v8go/deps/android_amd64 v0.0.0-20250515043113-5dcc98077472 // indirect
	github.com/tommie/v8go/deps/android_arm64 v0.0.0-20250515043113-5dcc98077472 // indirect
	github.com/tommie/v8go/deps/darwin_amd64 v0.0.0-20250515043113-5dcc98077472 // indirect
	github.com/tommie/v8go/deps/darwin_arm64 v0.0.0-20250515043113-5dcc98077472 // indirect
	github.com/tommie/v8go/deps/linux_amd64 v0.0.0-20250515043113-5dcc98077472 // indirect
	github.com/tommie/v8go/deps/linux_arm64 v0.0.0-20250515043113-5dcc98077472 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	gopkg.in/sourcemap.v1 v1.0.5 // indirect
	modernc.org/libc v1.72.3 // indirect
	modernc.org/libquickjs v0.12.8 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

// Local checkout — published goquickjs is the parent dir.
replace github.com/vimt/goquickjs => ..
