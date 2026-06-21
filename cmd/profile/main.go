// profile — runs a configurable mix of the benchmark workloads
// against goquickjs while writing a CPU profile to disk.
//
//	go run ./cmd/profile -bench fib -iters 50 -out cpu.prof
//	go tool pprof -top cpu.prof
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/pprof"
	"time"

	"github.com/vimt/goquickjs"
)

var scripts = map[string]string{
	"fib": `
		function fib(n) { return n < 2 ? n : fib(n-1) + fib(n-2); }
		let s = 0;
		for (let i = 0; i < 5; i++) s += fib(28);
		s
	`,
	"arith": `
		let s = 0;
		for (let i = 0; i < 200000; i++) {
			s += Math.sqrt(i) * 0.5 + i * 1.1;
		}
		Math.floor(s)
	`,
	"sort": `
		function sort(a) {
			let n = a.length;
			for (let i = 0; i < n; i++)
				for (let j = 0; j < n - 1 - i; j++)
					if (a[j] > a[j+1]) { let t = a[j]; a[j] = a[j+1]; a[j+1] = t; }
		}
		let a = [];
		for (let i = 0; i < 800; i++) a.push((i * 1103515245 + 12345) & 0x7fffffff);
		sort(a);
		a[0] + a[a.length-1]
	`,
	"strings": `
		let s = "";
		for (let i = 0; i < 5000; i++) s += "x" + i;
		s.length
	`,
	"objalloc": `
		function mk(n) {
			let arr = [];
			for (let i = 0; i < n; i++) arr.push({a: i, b: i*2, c: "k"+i});
			return arr.length;
		}
		mk(50000)
	`,
	// method calls on a user class: each a.dot / a.len2 resolves the
	// method on Vec.prototype, i.e. an own-miss + prototype walk on
	// every call — the workload proto-chain method ICs would target.
	"methods": `
		class Vec {
			constructor(x, y) { this.x = x; this.y = y; }
			dot(o)  { return this.x * o.x + this.y * o.y; }
			len2()  { return this.x * this.x + this.y * this.y; }
		}
		let a = new Vec(1, 2);
		let b = new Vec(3, 4);
		let s = 0;
		for (let i = 0; i < 300000; i++) {
			s = (s + a.dot(b) + a.len2() + b.len2()) % 1000000007;
		}
		s
	`,
}

func main() {
	bench := flag.String("bench", "fib", "comma-separated: fib,arith,sort,strings,objalloc,all")
	iters := flag.Int("iters", 5, "iterations per script")
	out := flag.String("out", "cpu.prof", "pprof output file")
	flag.Parse()

	f, err := os.Create(*out)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := pprof.StartCPUProfile(f); err != nil {
		panic(err)
	}
	defer pprof.StopCPUProfile()

	names := pick(*bench)
	for _, n := range names {
		src, ok := scripts[n]
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown bench: %s\n", n)
			continue
		}
		start := time.Now()
		for i := 0; i < *iters; i++ {
			if _, err := goquickjs.Eval(src); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", n, err)
				break
			}
		}
		fmt.Printf("%-10s %4d iters  %v\n", n, *iters, time.Since(start))
	}
	fmt.Printf("wrote %s\n", *out)
}

func pick(csv string) []string {
	if csv == "all" {
		return []string{"fib", "arith", "sort", "strings", "objalloc", "methods"}
	}
	var out []string
	for _, x := range splitCSV(csv) {
		out = append(out, x)
	}
	return out
}

func splitCSV(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}
