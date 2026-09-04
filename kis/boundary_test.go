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

func TestReadOnlyProductionBoundary(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(file))
	forbidden := regexp.MustCompile(`(?i)(order-cash|order-rvsecncl|\\b[A-Z]{3,}\\d+U\\b|\\b(Buy|Sell|Cancel|ReviseCancel|Do)\\b)`)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if forbidden.Match(raw) {
			t.Errorf("forbidden read/write surface in %s", path)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, raw, 0)
		if err != nil {
			return err
		}
		for _, decl := range parsed.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.IsExported() && regexp.MustCompile(`^(Buy|Sell|Cancel|ReviseCancel|Do)$`).MatchString(fn.Name.Name) {
				t.Errorf("public mutation symbol %s", fn.Name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
