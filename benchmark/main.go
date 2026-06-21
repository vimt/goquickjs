// Command jstest benchmarks embeddable scripting engines for Go across three
// languages — JavaScript (goja, sobek, paserati, quickjs, v8), Lua (gopher-lua)
// and Starlark (go.starlark.net) — on a set of portable algorithms (arith, fib,
// sort, strings) written equivalently in all three languages.
//
// Each algorithm computes the same integer checksum in every language (the math
// is chosen to stay exact across JS/Lua doubles and Starlark bignums), so the
// host can verify all engines agree before trusting the timings.
//
// Usage:
//
//	go run .                       # all engines, all benchmarks
//	go run . -scale 4              # 4x the work per benchmark
//	go run . -engines goja,gopher-lua,starlark
//	go run . -benchmarks sort,fib
//	go run . -mem                  # also measure peak / working-set memory
package main

import (
	"embed"
	"flag"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed bench/*
var benchFS embed.FS

// Engine is one embeddable scripting engine. Run executes a script in a FRESH
// VM and returns the script's `result` global (a checksum string).
type Engine struct {
	Name    string
	Lang    string // "js" | "lua" | "starlark"
	Version string
	Run     func(script string) (string, error)
}

var engineRegistry []Engine

func register(e Engine) { engineRegistry = append(engineRegistry, e) }

// benchSpec is one portable algorithm and its base work size (at scale 1.0).
type benchSpec struct {
	name  string
	baseN int
}

var benchmarks = []benchSpec{
	{"arith", 20000},
	{"fib", 20000},
	{"sort", 2000},
	{"strings", 4000},
}

func findSpec(name string) (benchSpec, bool) {
	for _, b := range benchmarks {
		if b.name == name {
			return b, true
		}
	}
	return benchSpec{}, false
}

func langExt(lang string) string {
	switch lang {
	case "js":
		return "js"
	case "lua":
		return "lua"
	case "starlark":
		return "star"
	case "tengo":
		return "tengo"
	case "anko":
		return "anko"
	case "go": // yaegi — not ".go", or the Go toolchain would compile it
		return "ygo"
	case "risor":
		return "risor"
	}
	return lang
}

// loadScript reads the benchmark source for a language and injects the work size.
func loadScript(lang, name string, n int) (string, error) {
	b, err := benchFS.ReadFile("bench/" + name + "." + langExt(lang))
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(b), "__N__", strconv.Itoa(n)), nil
}

// runConfig holds the knobs shared between in-process and child runs.
type runConfig struct {
	n          int
	warmup     bool
	benchmarks []string
	scale      float64
}

// engResult is one engine's outcome across all benchmarks.
type engResult struct {
	engine, lang, version string
	ok                    bool
	errMsg                string                   // engine-level failure (couldn't run anything)
	times                 map[string]time.Duration // per-benchmark median (only successful)
	results               map[string]string        // per-benchmark checksum (only successful)
	errs                  map[string]string        // per-benchmark failure (engine ran others fine)
	geomeanMs             float64

	// memory (only in -mem mode)
	memOK  bool
	netKB  int64 // default-GC peak RSS minus baseline
	workKB int64 // aggressive-GC working set minus baseline
}

// runEngineAll runs warmup + n timed iterations of every selected benchmark on
// one engine and returns the per-benchmark medians and checksums.
func runEngineAll(e Engine, cfg runConfig) engResult {
	r := engResult{
		engine: e.Name, lang: e.Lang, version: e.Version, ok: true,
		times:   map[string]time.Duration{},
		results: map[string]string{},
		errs:    map[string]string{},
	}
	for _, name := range cfg.benchmarks {
		spec, ok := findSpec(name)
		if !ok {
			r.errs[name] = "unknown benchmark"
			continue
		}
		n := int(float64(spec.baseN) * cfg.scale)
		if n < 1 {
			n = 1
		}
		src, err := loadScript(e.Lang, name, n)
		if err != nil {
			r.errs[name] = err.Error()
			continue
		}
		if cfg.warmup {
			if _, err := e.Run(src); err != nil {
				r.errs[name] = err.Error()
				continue
			}
		}
		var times []time.Duration
		var out string
		failed := false
		for i := 0; i < cfg.n; i++ {
			start := time.Now()
			o, err := e.Run(src)
			elapsed := time.Since(start)
			if err != nil {
				r.errs[name] = err.Error()
				failed = true
				break
			}
			out = o
			times = append(times, elapsed)
		}
		if failed {
			continue
		}
		r.times[name] = medianDur(times)
		r.results[name] = out
	}
	r.ok = len(r.times) > 0
	if !r.ok && r.errMsg == "" {
		r.errMsg = "all benchmarks failed"
	}
	r.geomeanMs = geomeanMs(cfg.benchmarks, r.times)
	return r
}

func medianDur(times []time.Duration) time.Duration {
	if len(times) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), times...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	return cp[len(cp)/2]
}

// geomeanMs is the geometric mean of the per-benchmark median times, in ms.
func geomeanMs(names []string, times map[string]time.Duration) float64 {
	var sumLog float64
	var n int
	for _, name := range names {
		ms := float64(times[name]) / float64(time.Millisecond)
		if ms <= 0 {
			continue
		}
		sumLog += math.Log(ms)
		n++
	}
	if n == 0 {
		return 0
	}
	return math.Exp(sumLog / float64(n))
}

func main() {
	n := flag.Int("n", 3, "number of timed iterations per benchmark")
	warmup := flag.Bool("warmup", true, "run one untimed warmup iteration first")
	enginesFlag := flag.String("engines", "", "comma-separated engine subset (default: all)")
	benchFlag := flag.String("benchmarks", "", "comma-separated benchmark subset (default: all)")
	scale := flag.Float64("scale", 1.0, "work multiplier per benchmark (bigger = longer)")
	mem := flag.Bool("mem", false, "also measure peak/working-set memory (per-engine subprocess, ru_maxrss)")
	size := flag.Bool("size", false, "measure binary size added per engine (builds one minimal binary per engine)")
	child := flag.String("child", "", "internal: run one engine in subprocess mode and emit a JSON line")
	childGC := flag.Int("child-gc", -1, "internal: SetGCPercent for the child (>=0 enables the working-set pass)")
	flag.Parse()

	benchNames := benchNamesAll()
	if *benchFlag != "" {
		benchNames = splitCSV(*benchFlag)
	}
	cfg := runConfig{n: *n, warmup: *warmup, benchmarks: benchNames, scale: *scale}

	if *child != "" {
		runChild(*child, cfg, *childGC)
		return
	}

	engines := selectEngines(*enginesFlag)

	if *size {
		runSizeProbe(engines)
		return
	}

	fmt.Printf("# scripting engine benchmark (portable cross-language workloads)\n")
	fmt.Printf("Go %s  %s/%s  CPU=%d\n", runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	fmt.Printf("benchmarks: %s\n", strings.Join(benchNames, ", "))
	fmt.Printf("iterations: %d (warmup=%v)  scale: %g\n", *n, *warmup, *scale)
	if *mem {
		fmt.Printf("mem mode: per-engine subprocess, peak/working-set RSS via ru_maxrss\n")
	}
	fmt.Println()

	var results []engResult
	if *mem {
		results = runWithMem(engines, cfg)
	} else {
		for _, e := range engines {
			fmt.Printf(">> %-12s %-9s (%s) ", e.Name, "["+e.Lang+"]", e.Version)
			r := runEngineAll(e, cfg)
			switch {
			case !r.ok:
				detail := r.errMsg
				for _, b := range sortedKeys(r.errs) {
					detail = b + ": " + r.errs[b]
					break
				}
				fmt.Printf("FAILED: %v\n", trunc(detail, 200))
			case len(r.errs) > 0:
				fmt.Printf("geomean=%.1fms (failed: %s)\n", r.geomeanMs, strings.Join(sortedKeys(r.errs), ","))
			default:
				fmt.Printf("geomean=%.1fms\n", r.geomeanMs)
			}
			results = append(results, r)
		}
	}

	printSummary(results, benchNames, *mem)
	printConsistency(results, benchNames)
}

func benchNamesAll() []string {
	out := make([]string, len(benchmarks))
	for i, b := range benchmarks {
		out[i] = b.name
	}
	return out
}

func selectEngines(csv string) []Engine {
	if csv == "" {
		return engineRegistry
	}
	want := map[string]bool{}
	for _, e := range splitCSV(csv) {
		want[e] = true
	}
	var filtered []Engine
	for _, e := range engineRegistry {
		if want[strings.ToLower(e.Name)] {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func printSummary(results []engResult, benchNames []string, showMem bool) {
	fmt.Printf("\n## Per-benchmark median time, ms (lower better)\n\n")
	fmt.Printf("%-12s %-9s", "engine", "lang")
	for _, b := range benchNames {
		fmt.Printf(" %10s", trunc(b, 10))
	}
	fmt.Printf(" %10s\n", "geomean")
	fmt.Printf("%s\n", strings.Repeat("-", 22+11*(len(benchNames)+1)))
	for _, r := range results {
		fmt.Printf("%-12s %-9s", r.engine, "["+r.lang+"]")
		if !r.ok {
			fmt.Printf("  FAILED: %s\n", trunc(r.errMsg, 60))
			continue
		}
		for _, b := range benchNames {
			if _, ok := r.times[b]; ok {
				fmt.Printf(" %10s", fmtMs(r.times[b]))
			} else if _, bad := r.errs[b]; bad {
				fmt.Printf(" %10s", "ERR")
			} else {
				fmt.Printf(" %10s", "-")
			}
		}
		fmt.Printf(" %10.1f\n", r.geomeanMs)
	}

	if showMem {
		fmt.Printf("\n## Memory, net of runtime baseline (lower better)\n\n")
		fmt.Printf("%-12s %-9s %12s %12s\n", "engine", "lang", "peak(net)", "work(net)")
		fmt.Printf("%s\n", strings.Repeat("-", 48))
		for _, r := range results {
			peak, work := "-", "-"
			if r.memOK {
				peak = humanKB(r.netKB)
				if r.workKB > 0 {
					work = humanKB(r.workKB)
				}
			}
			fmt.Printf("%-12s %-9s %12s %12s\n", r.engine, "["+r.lang+"]", peak, work)
		}
	}
}

// printConsistency verifies every engine produced the same checksum per
// benchmark — the cross-language correctness check.
func printConsistency(results []engResult, benchNames []string) {
	fmt.Printf("\n## Cross-language consistency (all engines must agree)\n\n")
	for _, b := range benchNames {
		vals := map[string][]string{} // checksum -> engines
		for _, r := range results {
			if !r.ok {
				continue
			}
			if v, ok := r.results[b]; ok {
				vals[v] = append(vals[v], r.engine)
			}
		}
		if len(vals) == 0 {
			fmt.Printf("  %-10s no data\n", b)
			continue
		}
		if len(vals) == 1 {
			var checksum string
			for k := range vals {
				checksum = k
			}
			fmt.Printf("  %-10s OK  (checksum=%s)\n", b, trunc(checksum, 20))
			continue
		}
		fmt.Printf("  %-10s MISMATCH:\n", b)
		for v, engs := range vals {
			fmt.Printf("      %s <- %s\n", trunc(v, 24), strings.Join(engs, ","))
		}
	}
}

func fmtMs(d time.Duration) string {
	ms := float64(d) / float64(time.Millisecond)
	if ms == 0 {
		return "-"
	}
	return strconv.FormatFloat(ms, 'f', 2, 64)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.ToLower(p))
		}
	}
	return out
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
