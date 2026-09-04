package kis

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var boundaryForbidden = regexp.MustCompile(`(?i)(order-cash|order-rvsecncl|\b[A-Z]{3,}\d+U\b)`)
var boundaryMethods = regexp.MustCompile(`^(Buy|Sell|Cancel|ReviseCancel|Do)$`)

func boundaryViolations(root string) ([]string, error) {
	var violations []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if boundaryForbidden.Match(raw) {
			violations = append(violations, path+": source")
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, raw, 0)
		if err != nil {
			return err
		}
		for _, decl := range parsed.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.IsExported() && boundaryMethods.MatchString(fn.Name.Name) {
				violations = append(violations, path+": "+fn.Name.Name)
			}
		}
		return nil
	})
	return violations, err
}

func TestReadOnlyProductionBoundary(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	violations, err := boundaryViolations(filepath.Dir(filepath.Dir(file)))
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("forbidden read/write surface: %v", violations)
	}
}

func TestBoundaryTripwireProbes(t *testing.T) {
	for _, probe := range []struct {
		name, source string
		want         bool
	}{
		{"receiver mutation", "package probe\ntype Client struct{}\nfunc (*Client) Buy() {}\n", true},
		{"U mapping", "package probe\nconst transaction = \"TTTC0012U\"\n", true},
		{"read history", "package probe\ntype OrderHistory struct{}\nfunc ReadHistory() {}\n", false},
	} {
		t.Run(probe.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "probe.go")
			if err := os.WriteFile(path, []byte(probe.source), 0600); err != nil {
				t.Fatal(err)
			}
			violations, err := boundaryViolations(root)
			if err != nil {
				t.Fatal(err)
			}
			if (len(violations) > 0) != probe.want {
				t.Fatalf("violations=%v wantForbidden=%v", violations, probe.want)
			}
		})
	}
}
