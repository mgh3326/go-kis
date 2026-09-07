package ws_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mgh3326/go-kis/kis/ws"
)

// AC3: the exported protocol error vocabulary is closed. Growing it is a
// breaking change for every consumer's switch, so it is asserted here rather
// than left to review.
func TestErrorVocabularyIsClosed(t *testing.T) {
	want := []string{"ErrApprovalRejected", "ErrSessionOccupied", "ErrSubscribeFailed", "SubscribeError"}

	var found []string
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			switch node := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range node.Specs {
					switch typed := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range typed.Names {
							if name.IsExported() && strings.HasPrefix(name.Name, "Err") {
								found = append(found, name.Name)
							}
						}
					case *ast.TypeSpec:
						if typed.Name.IsExported() && strings.HasSuffix(typed.Name.Name, "Error") {
							found = append(found, typed.Name.Name)
						}
					}
				}
			case *ast.FuncDecl:
				// An exported Error() method would add a public error type.
				if node.Recv != nil && node.Name.Name == "Error" && node.Type.Params.NumFields() == 0 {
					if name := receiverName(node); name != "" && ast.IsExported(name) {
						found = append(found, name)
					}
				}
			}
		}
	}
	slices.Sort(found)
	found = slices.Compact(found)
	if !slices.Equal(found, want) {
		t.Fatalf("exported error vocabulary = %v, want exactly %v", found, want)
	}
}

func receiverName(fn *ast.FuncDecl) string {
	if len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// Every SubscribeError matches the sentinel, so consumers can switch on the
// class without depending on the concrete type.
func TestSubscribeErrorSentinel(t *testing.T) {
	err := error(&ws.SubscribeError{TR: "H0STCNT0", RTCD: "1", MsgCD: "MCA00101", Msg1: "INVALID"})
	if !errors.Is(err, ws.ErrSubscribeFailed) {
		t.Fatal("SubscribeError does not match ErrSubscribeFailed")
	}
	if errors.Is(err, ws.ErrSessionOccupied) || errors.Is(err, ws.ErrApprovalRejected) {
		t.Fatal("SubscribeError must not match the other sentinels")
	}
	if !strings.Contains(err.Error(), "MCA00101") {
		t.Fatalf("Error() = %q, want the msg_cd", err.Error())
	}
}

// AC: no credential or key material ever reaches an error string or an event.
// The approval key is the only secret this package holds, and it belongs on
// the wire and nowhere else.
func TestSecretsStayOffTheErrorPath(t *testing.T) {
	const secret = "super-secret-approval-key"
	server := newServer()
	server.setReply(func(conn *fakeTransport, request wireRequest, _ []byte) {
		conn.push(`{"header":{"tr_id":"` + request.Body.Input.TRID + `"},"body":{"rt_cd":"1","msg_cd":"MCA00101","msg1":"NO","output":{"iv":"SAMPLE-IV-000000","key":"AES-SAMPLE-AES-SAMPLE-AES-SAMPLE"}}}`)
	})
	conn := dialTest(t, ws.Config{Approval: &staticProvider{key: secret}, Dialer: server})
	err := conn.Subscribe(t.Context(), ws.TRQuotePrice, "005930")
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error message leaked the approval key: %s", err)
	}
	if stats := conn.Stats(); strings.Contains(stats.LastDropReason, "SAMPLE-IV-000000") {
		t.Fatalf("drop reason leaked key material: %+v", stats)
	}
}

// The published source carries no live-looking credential or account value.
func TestNoCredentialsInPackageSource(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"appsecret\":", "PSAK", "approval_key\":\"P"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("%s contains %q", path, forbidden)
			}
		}
	}
}
