package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// engineTag maps an engine Name to its sizeprobe build tag (see the
// `//go:build !sizeprobe || eng_*` constraints atop each engine file).
func engineTag(name string) string {
	return "eng_" + strings.ReplaceAll(strings.ToLower(name), "-", "")
}

// runSizeProbe builds a minimal binary with exactly one engine linked in, for
// each engine, and reports the size and the delta over an engine-free baseline —
// i.e. how much embedding each engine adds to your program. It shells out to
// `go build` with build tags; v8 is skipped (its cgo static libv8 needs the
// special build and is reported separately).
func runSizeProbe(engines []Engine) {
	tmp := filepath.Join(os.TempDir(), "jstest-sizeprobe.bin")
	defer os.Remove(tmp)

	build := func(tags string) (int64, error) {
		cmd := exec.Command("go", "build", "-tags", tags, "-o", tmp, ".")
		cmd.Env = append(os.Environ(), "GOWORK=off")
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return 0, err
		}
		fi, err := os.Stat(tmp)
		if err != nil {
			return 0, err
		}
		return fi.Size(), nil
	}

	fmt.Printf("building baseline (no engine)...\n")
	base, err := build("sizeprobe")
	if err != nil {
		fmt.Printf("baseline build failed: %v\n", err)
		return
	}

	type row struct {
		name, lang  string
		size, delta int64
	}
	var rows []row
	for _, e := range engines {
		if strings.EqualFold(e.Name, "v8") {
			continue // cgo + static libv8, measured separately
		}
		fmt.Printf("building %-12s ...\n", e.Name)
		sz, err := build("sizeprobe," + engineTag(e.Name))
		if err != nil {
			fmt.Printf("  %s build failed: %v\n", e.Name, err)
			continue
		}
		rows = append(rows, row{e.Name, e.Lang, sz, sz - base})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].delta < rows[j].delta })

	fmt.Printf("\n## Binary size added per engine (lower better)\n\n")
	fmt.Printf("engine-free baseline: %s\n\n", humanBytes(base))
	fmt.Printf("%-12s %-9s %12s %12s\n", "engine", "lang", "binary", "added")
	fmt.Printf("%s\n", strings.Repeat("-", 48))
	for _, r := range rows {
		fmt.Printf("%-12s %-9s %12s %12s\n", r.name, "["+r.lang+"]", humanBytes(r.size), "+"+humanBytes(r.delta))
	}
	fmt.Printf("\n(v8 skipped: cgo + static libv8 adds ~80MB; build with -tags v8 to see.)\n")
}

func humanBytes(b int64) string {
	return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
}
