package main

import "runtime/debug"

// moduleVersion returns the resolved version of a dependency module, read from
// the build info embedded in the binary. Falls back to "?" if unavailable.
func moduleVersion(path string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "?"
	}
	for _, d := range bi.Deps {
		if d.Path == path {
			v := d.Version
			if d.Replace != nil {
				v = d.Replace.Version
			}
			return v
		}
	}
	return "?"
}
