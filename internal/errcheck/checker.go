package errcheck

import (
	"fmt"
	"go/ast"
	"golang.org/x/tools/go/packages"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type Checker struct {
	// ignore is a map of package names to regular expressions. Identifiers from a package are
	// checked against its regular expressions and if any of the expressions match the call
	// is not checked.
	Ignore map[string]*regexp.Regexp

	// If blank is true then assignments to the blank identifier are also considered to be
	// ignored errors.
	Blank bool

	// If asserts is true then ignored type assertion results are also checked
	Asserts bool

	// build tags
	Tags []string

	Verbose bool

	// If true, checking of _test.go files is disabled
	WithoutTests bool

	// If true, checking of files with generated code is disabled
	WithoutGeneratedCode bool

	exclude map[string]bool
}

func NewChecker() *Checker {
	c := Checker{}
	c.SetExclude(map[string]bool{})
	return &c
}

func (c *Checker) SetExclude(l map[string]bool) {
	c.exclude = map[string]bool{}

	// Default exclude for stdlib functions
	for _, exc := range []string{
		// bytes
		"(*bytes.Buffer).Write",
		"(*bytes.Buffer).WriteByte",
		"(*bytes.Buffer).WriteRune",
		"(*bytes.Buffer).WriteString",

		// fmt
		"fmt.Errorf",
		"fmt.Print",
		"fmt.Printf",
		"fmt.Println",
		"fmt.Fprint(*bytes.Buffer)",
		"fmt.Fprintf(*bytes.Buffer)",
		"fmt.Fprintln(*bytes.Buffer)",
		"fmt.Fprint(*strings.Builder)",
		"fmt.Fprintf(*strings.Builder)",
		"fmt.Fprintln(*strings.Builder)",
		"fmt.Fprint(os.Stderr)",
		"fmt.Fprintf(os.Stderr)",
		"fmt.Fprintln(os.Stderr)",

		// math/rand
		"math/rand.Read",
		"(*math/rand.Rand).Read",

		// strings
		"(*strings.Builder).Write",
		"(*strings.Builder).WriteByte",
		"(*strings.Builder).WriteRune",
		"(*strings.Builder).WriteString",

		// hash
		"(hash.Hash).Write",
	} {
		c.exclude[exc] = true
	}

	for k := range l {
		c.exclude[k] = true
	}
}

func (c *Checker) logf(msg string, args ...interface{}) {
	if c.Verbose {
		fmt.Fprintf(os.Stderr, msg+"\n", args...)
	}
}

// loadPackages is used for testing.
var loadPackages = func(cfg *packages.Config, paths ...string) ([]*packages.Package, error) {
	return packages.Load(cfg, paths...)
}

func (c *Checker) load(paths ...string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode:       packages.LoadAllSyntax,
		Tests:      !c.WithoutTests,
		BuildFlags: []string{fmt.Sprintf("-tags=%s", strings.Join(c.Tags, " "))},
	}
	return loadPackages(cfg, paths...)
}

var generatedCodeRegexp = regexp.MustCompile("^// Code generated .* DO NOT EDIT\\.$")

func (c *Checker) shouldSkipFile(file *ast.File) bool {
	if !c.WithoutGeneratedCode {
		return false
	}

	for _, cg := range file.Comments {
		for _, comment := range cg.List {
			if generatedCodeRegexp.MatchString(comment.Text) {
				return true
			}
		}
	}

	return false
}

// CheckPackages checks packages for errors.
func (c *Checker) CheckPackages(paths ...string) error {
	pkgs, err := c.load(paths...)
	if err != nil {
		return err
	}
	// Check for errors in the initial packages.
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return fmt.Errorf("errors while loading package %s: %v", pkg.ID, pkg.Errors)
		}
	}

	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	go111module := (err == nil) && strings.TrimSpace(string(gomod)) != ""
	ignore := c.Ignore
	if go111module {
		ignore = make(map[string]*regexp.Regexp)
		for pkg, re := range c.Ignore {
			if nonVendoredPkg, ok := nonVendoredPkgPath(pkg); ok {
				ignore[nonVendoredPkg] = re
			} else {
				ignore[pkg] = re
			}
		}
	}

	var wg sync.WaitGroup
	u := &UncheckedErrors{}
	for _, pkg := range pkgs {
		wg.Add(1)

		go func(pkg *packages.Package) {
			defer wg.Done()
			c.logf("Checking %s", pkg.Types.Path())

			v := &visitor{
				pkg:         pkg,
				ignore:      ignore,
				blank:       c.Blank,
				asserts:     c.Asserts,
				lines:       make(map[string][]string),
				exclude:     c.exclude,
				go111module: go111module,
				traces:      []*scopes{{ /* root level scope */ }},
				errors:      []UncheckedError{},
			}

			for _, astFile := range v.pkg.Syntax {
				if c.shouldSkipFile(astFile) {
					continue
				}
				ast.Walk(v, astFile)
			}
			u.Append(v.errors...)
		}(pkg)
	}

	wg.Wait()
	if u.Len() > 0 {
		// Sort unchecked errors and remove duplicates. Duplicates may occur when a file
		// containing an unchecked error belongs to > 1 package.
		sort.Sort(byName{u})
		uniq := u.Errors[:0] // compact in-place
		for i, err := range u.Errors {
			if i == 0 || err != u.Errors[i-1] {
				uniq = append(uniq, err)
			}
		}
		u.Errors = uniq
		return u
	}
	return nil
}
