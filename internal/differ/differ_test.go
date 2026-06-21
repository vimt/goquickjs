package differ

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorpus(t *testing.T) {
	matches, err := filepath.Glob("testdata/*.js")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("no testdata/*.js fixtures found")
	}

	var oracle Evaluator = QuickJSOracle{}
	var sut Evaluator = GoQuickJSSUT{}

	for _, path := range matches {
		name := strings.TrimSuffix(filepath.Base(path), ".js")
		t.Run(name, func(t *testing.T) {
			srcBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			src := string(srcBytes)

			d := Run(oracle, sut, src)

			if d.SkipNYI() {
				t.Skipf("SUT NYI; %s returned %q (err=%v)",
					oracle.Name(), d.Oracle.Value, d.Oracle.Err)
				return
			}

			if !d.Equal() {
				t.Fatalf("divergence:\n--- source ---\n%s\n--- %s ---\nvalue=%q err=%v\n--- %s ---\nvalue=%q err=%v",
					src,
					oracle.Name(), d.Oracle.Value, d.Oracle.Err,
					sut.Name(), d.SUT.Value, d.SUT.Err)
			}
		})
	}
}
