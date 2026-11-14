package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

const (
	busPkg      = "clubmate/internal/bus"
	testutilPkg = "clubmate/internal/testutil"

	msgSleep    = "fixed sleep: wait for the condition with testutil.Eventually, or prove absence with testutil.Never"
	msgAfter    = "unscaled deadline: wrap it in testutil.Timeout, or wait with testutil.Eventually"
	msgTestify  = "unscaled deadline: use testutil.Eventually / Poll / Never"
	msgReadOnce = "read-once: wait with testutil.WaitEvents"
	msgTempPath = "shared fixed temp path: use t.TempDir or testutil.ShortDirs"
)

var Analyzer = &analysis.Analyzer{
	Name: "testlint",
	Doc:  "reports flaky test patterns: fixed sleeps, unscaled testify waits, read-once bus reads and shared fixed temp paths",
	Run:  run,
}

var allowedSleepers = map[string]bool{
	"clubmate/internal/cli.syncBuffer.Write":       true,
	"clubmate/internal/sessionhost.throttled.Read": true,
	"clubmate/internal/macui.instance.eventually":  true,
}

var testifyWaits = map[string]bool{
	"Eventually":       true,
	"Eventuallyf":      true,
	"EventuallyWithT":  true,
	"EventuallyWithTf": true,
	"Never":            true,
	"Neverf":           true,
}

func run(pass *analysis.Pass) (any, error) {
	pkg := strings.TrimSuffix(pass.Pkg.Path(), "_test")
	if pkg == testutilPkg {
		return nil, nil
	}
	for _, file := range pass.Files {
		if !strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				checkBody(pass, fn.Body, !allowedSleepers[pkg+"."+methodName(fn)], pkg == busPkg)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			checkAnywhere(pass, n)
			return true
		})
	}
	return nil, nil
}

func checkBody(pass *analysis.Pass, body *ast.BlockStmt, sleeps, inBusPkg bool) {
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
		if !ok {
			return true
		}
		switch {
		case isSubtest(fn):
			for _, arg := range call.Args {
				if lit, ok := arg.(*ast.FuncLit); ok {
					checkBody(pass, lit.Body, sleeps, inBusPkg)
				}
			}
		case sleeps && isPkgFunc(fn, "time", "Sleep"):
			pass.Reportf(call.Pos(), msgSleep)
		case isPkgFunc(fn, "time", "After") && !isScaled(pass, call):
			pass.Reportf(call.Pos(), msgAfter)
		case !inBusPkg && isBusRead(fn):
			pass.Reportf(call.Pos(), msgReadOnce)
		}
		return true
	})
}

func checkAnywhere(pass *analysis.Pass, n ast.Node) {
	switch n := n.(type) {
	case *ast.CallExpr:
		fn, ok := typeutil.Callee(pass.TypesInfo, n).(*types.Func)
		if !ok || fn.Pkg() == nil {
			return
		}
		switch {
		case isTestify(fn) && testifyWaits[fn.Name()]:
			pass.Reportf(n.Pos(), msgTestify)
		case isPkgFunc(fn, "path/filepath", "Join") && len(n.Args) >= 2 && isTempDir(pass, n.Args[0]) && isStringLit(n.Args[1]):
			pass.Reportf(n.Pos(), msgTempPath)
		}
	case *ast.BinaryExpr:
		if n.Op == token.ADD && isTempDir(pass, n.X) && isStringLit(n.Y) {
			pass.Reportf(n.Pos(), msgTempPath)
		}
	}
}

func methodName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if idx, ok := t.(*ast.IndexExpr); ok {
		t = idx.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

func isScaled(pass *analysis.Pass, call *ast.CallExpr) bool {
	if len(call.Args) != 1 {
		return false
	}
	inner, ok := ast.Unparen(call.Args[0]).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := typeutil.Callee(pass.TypesInfo, inner).(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == testutilPkg && fn.Name() == "Timeout"
}

func isPkgFunc(fn *types.Func, pkg, name string) bool {
	return fn.Pkg() != nil && fn.Pkg().Path() == pkg && fn.Name() == name && fn.Signature().Recv() == nil
}

func isSubtest(fn *types.Func) bool {
	recv := fn.Signature().Recv()
	if recv == nil || fn.Name() != "Run" || fn.Pkg() == nil || fn.Pkg().Path() != "testing" {
		return false
	}
	ptr, ok := recv.Type().(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	return ok && named.Obj().Name() == "T"
}

func isTestify(fn *types.Func) bool {
	p := fn.Pkg().Path()
	return p == "github.com/stretchr/testify/require" || p == "github.com/stretchr/testify/assert"
}

func isBusRead(fn *types.Func) bool {
	recv := fn.Signature().Recv()
	if recv == nil || fn.Name() != "Read" {
		return false
	}
	ptr, ok := recv.Type().(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	return ok && named.Obj().Name() == "Bus" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == busPkg
}

func isTempDir(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	return ok && isPkgFunc(fn, "os", "TempDir")
}

func isStringLit(e ast.Expr) bool {
	lit, ok := ast.Unparen(e).(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}
