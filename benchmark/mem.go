package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// jsonSentinel prefixes the single machine-readable line a child prints.
const jsonSentinel = "@@JSTEST_JSON@@ "

// tightGCPercent is the aggressive GC setting for the "working set" pass: it
// forces Go to collect early so peak RSS reflects the live object set rather
// than garbage the default GC is too lazy to reclaim. It only affects the Go
// heap — engines that keep objects off it (quickjs, v8) are unchanged, which is
// what makes the two passes comparable.
const tightGCPercent = 10

// childResult is what a child subprocess reports back to the parent.
type childResult struct {
	Engine    string            `json:"engine"`
	Lang      string            `json:"lang"`
	Version   string            `json:"version"`
	OK        bool              `json:"ok"`
	Err       string            `json:"err"`
	GeomeanMs float64           `json:"geomeanMs"`
	TimesNs   map[string]int64  `json:"timesNs"`
	Results   map[string]string `json:"results"`
	Errs      map[string]string `json:"errs"`
}

// runChild runs all benchmarks for one engine (or the "__baseline__" no-op) and
// emits one JSON line. gcPercent >= 0 applies aggressive GC (working-set pass).
func runChild(name string, cfg runConfig, gcPercent int) {
	if gcPercent >= 0 {
		debug.SetGCPercent(gcPercent)
	}
	if name == "__baseline__" {
		emitChild(childResult{Engine: name, OK: true})
		return
	}

	var target *Engine
	for i := range engineRegistry {
		if strings.EqualFold(engineRegistry[i].Name, name) {
			target = &engineRegistry[i]
			break
		}
	}
	if target == nil {
		emitChild(childResult{Engine: name, OK: false, Err: "unknown engine"})
		return
	}

	r := runEngineAll(*target, cfg)
	cr := childResult{
		Engine: r.engine, Lang: r.lang, Version: r.version,
		OK: r.ok, Err: r.errMsg, GeomeanMs: r.geomeanMs,
		TimesNs: map[string]int64{}, Results: r.results, Errs: r.errs,
	}
	for k, v := range r.times {
		cr.TimesNs[k] = int64(v)
	}
	emitChild(cr)
}

func emitChild(cr childResult) {
	b, _ := json.Marshal(cr)
	fmt.Println(jsonSentinel + string(b))
}

// runWithMem runs each engine in its own subprocess twice: once with the default
// GC (peak RSS you actually pay) and once with aggressive GC + madvdontneed (an
// approximate live working set). A no-op baseline child measures the runtime
// floor, subtracted from both.
func runWithMem(engines []Engine, cfg runConfig) []engResult {
	baseKB, err := spawnChildRSS("__baseline__", cfg, -1, nil)
	if err != nil {
		fmt.Printf("warning: baseline measurement failed: %v\n", err)
		baseKB = 0
	} else {
		fmt.Printf("baseline (runtime floor): %s\n", humanKB(baseKB))
	}
	netOf := func(rssKB int64) int64 {
		if n := rssKB - baseKB; n > 0 {
			return n
		}
		return 0
	}
	tightEnv := append(os.Environ(), "GODEBUG=madvdontneed=1")

	var results []engResult
	for _, e := range engines {
		fmt.Printf(">> %-12s %-9s (%s) ", e.Name, "["+e.Lang+"]", e.Version)

		cr, peakRSS, err := spawnChild(e.Name, cfg, -1, nil)
		if err != nil {
			fmt.Printf("FAILED to spawn: %v\n", trunc(err.Error(), 120))
			results = append(results, engResult{engine: e.Name, lang: e.Lang, version: e.Version, ok: false, errMsg: err.Error()})
			continue
		}

		r := engResult{
			engine: firstNonEmpty(cr.Engine, e.Name), lang: firstNonEmpty(cr.Lang, e.Lang),
			version: firstNonEmpty(cr.Version, e.Version),
			ok:      cr.OK, errMsg: cr.Err, geomeanMs: cr.GeomeanMs,
			results: cr.Results, errs: cr.Errs,
			times: map[string]time.Duration{},
			memOK: true, netKB: netOf(peakRSS),
		}
		for k, ns := range cr.TimesNs {
			r.times[k] = time.Duration(ns)
		}

		if _, workRSS, werr := spawnChild(e.Name, cfg, tightGCPercent, tightEnv); werr == nil {
			r.workKB = netOf(workRSS)
		}

		if r.ok {
			fmt.Printf("geomean=%.1fms peak(net)=%s work(net)=%s\n", r.geomeanMs, humanKB(r.netKB), humanKB(r.workKB))
		} else {
			fmt.Printf("FAILED: %v\n", trunc(r.errMsg, 160))
		}
		results = append(results, r)
	}
	return results
}

// spawnChild execs ourselves in -child mode for one engine and returns its
// reported result plus peak RSS in KB. gcPercent >= 0 forces aggressive GC;
// extraEnv (if non-nil) replaces the child environment.
func spawnChild(name string, cfg runConfig, gcPercent int, extraEnv []string) (childResult, int64, error) {
	args := []string{"-child", name, "-child-gc", strconv.Itoa(gcPercent)}
	args = append(args, passthroughFlags(cfg)...)

	cmd := exec.Command(os.Args[0], args...)
	cmd.Stderr = os.Stderr
	cmd.Env = extraEnv // nil => inherit parent environment
	out, err := cmd.Output()

	var rssKB int64
	if cmd.ProcessState != nil {
		if ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
			rssKB = int64(ru.Maxrss) // Linux: kilobytes
		}
	}
	if err != nil {
		return childResult{}, rssKB, err
	}
	cr, perr := parseChild(out)
	if perr != nil {
		return childResult{}, rssKB, perr
	}
	return cr, rssKB, nil
}

func spawnChildRSS(name string, cfg runConfig, gcPercent int, extraEnv []string) (int64, error) {
	_, rssKB, err := spawnChild(name, cfg, gcPercent, extraEnv)
	return rssKB, err
}

func parseChild(out []byte) (childResult, error) {
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, jsonSentinel) {
			var cr childResult
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, jsonSentinel)), &cr); err != nil {
				return childResult{}, fmt.Errorf("bad child JSON: %w", err)
			}
			return cr, nil
		}
	}
	return childResult{}, fmt.Errorf("no result line from child")
}

func passthroughFlags(cfg runConfig) []string {
	return []string{
		"-n", strconv.Itoa(cfg.n),
		"-warmup=" + strconv.FormatBool(cfg.warmup),
		"-benchmarks", strings.Join(cfg.benchmarks, ","),
		"-scale", strconv.FormatFloat(cfg.scale, 'g', -1, 64),
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func humanKB(kb int64) string {
	if kb <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1fMB", float64(kb)/1024.0)
}
