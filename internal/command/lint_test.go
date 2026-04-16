package command_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoExternalExecImports(t *testing.T) {
	// Walk all .go files in the module and verify that "os/exec" is only
	// imported by files within internal/command/.
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip vendor, .git, testdata
			base := filepath.Base(path)
			if base == "vendor" || base == ".git" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return nil // skip unparseable files
		}

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == "os/exec" {
				rel, _ := filepath.Rel(root, path)
				// Allow internal/command/ itself
				if !strings.HasPrefix(rel, filepath.Join("internal", "command")) {
					t.Errorf("%s imports \"os/exec\" — all exec usage must go through internal/command", rel)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
