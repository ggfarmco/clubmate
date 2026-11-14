package main

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var testName = regexp.MustCompile(`^Test([^a-z].*)?$`)

type pkgTests struct {
	dir   string
	tests []string
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: changedtests <base>")
		os.Exit(2)
	}
	pkgs, err := changed(".", os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "changedtests:", err)
		os.Exit(1)
	}
	for _, p := range pkgs {
		fmt.Printf("./%s\t^(%s)$\n", p.dir, strings.Join(p.tests, "|"))
	}
}

type change struct {
	old, cur string
}

func changed(repo, base string) ([]pkgTests, error) {
	files, err := changedFiles(repo, base)
	if err != nil {
		return nil, err
	}
	byDir := map[string][]string{}
	for _, f := range files {
		if skipped(repo, f.cur) {
			continue
		}
		cur, err := os.ReadFile(filepath.Join(repo, f.cur))
		if err != nil {
			return nil, err
		}
		var old []byte
		if f.old != "" {
			old, _ = git(repo, "show", base+":"+f.old)
		}
		tests, err := changedTests(old, cur)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.cur, err)
		}
		dir := filepath.Dir(f.cur)
		byDir[dir] = append(byDir[dir], tests...)
	}
	var pkgs []pkgTests
	for dir, tests := range byDir {
		if len(tests) == 0 {
			continue
		}
		slices.Sort(tests)
		pkgs = append(pkgs, pkgTests{dir: dir, tests: slices.Compact(tests)})
	}
	slices.SortFunc(pkgs, func(a, b pkgTests) int {
		return cmp.Or(cmp.Compare(len(a.tests), len(b.tests)), strings.Compare(a.dir, b.dir))
	})
	return pkgs, nil
}

func changedFiles(repo, base string) ([]change, error) {
	out, err := git(repo, "diff", "-z", "--name-status", "-M", "--diff-filter=d", base, "--", "*_test.go")
	if err != nil {
		return nil, err
	}
	var files []change
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		switch status := fields[i]; {
		case strings.HasPrefix(status, "R") && i+2 < len(fields):
			files = append(files, change{old: fields[i+1], cur: fields[i+2]})
			i++
		case status == "A":
			files = append(files, change{cur: fields[i+1]})
		default:
			files = append(files, change{old: fields[i+1], cur: fields[i+1]})
		}
	}
	untracked, err := git(repo, "ls-files", "-z", "--others", "--exclude-standard", "--", "*_test.go")
	if err != nil {
		return nil, err
	}
	for path := range strings.SplitSeq(string(untracked), "\x00") {
		if path != "" {
			files = append(files, change{cur: path})
		}
	}
	return files, nil
}

func skipped(repo, path string) bool {
	dir := filepath.Dir(path)
	if dir == "." {
		return false
	}
	parts := strings.Split(dir, "/")
	if slices.Contains(parts, "testdata") || slices.Contains(parts, "dist") {
		return true
	}
	for i := len(parts); i > 0; i-- {
		if _, err := os.Stat(filepath.Join(repo, filepath.Join(parts[:i]...), "go.mod")); err == nil {
			return true
		}
	}
	return false
}

func changedTests(old, head []byte) ([]string, error) {
	headTests, err := tests(head)
	if err != nil {
		return nil, err
	}
	oldTests, _ := tests(old)
	var picked []string
	for _, name := range slices.Sorted(maps.Keys(headTests)) {
		if prev, ok := oldTests[name]; !ok || prev != headTests[name] {
			picked = append(picked, name)
		}
	}
	return picked, nil
}

func tests(src []byte) (map[string]string, error) {
	if src == nil {
		return nil, fmt.Errorf("no source")
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	found := map[string]string{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !testName.MatchString(fn.Name.Name) || fn.Name.Name == "TestMain" {
			continue
		}
		found[fn.Name.Name] = string(src[fset.Position(fn.Pos()).Offset:fset.Position(fn.End()).Offset])
	}
	return found, nil
}

func git(repo string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
