// Package-level parallel scheduling and the parallel parse of one package's
// files.

package build

import (
	"runtime"
	"sync"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/parser"
	"zyx/internal/source"
)

// parseFiles parses the files of one package in parallel. Files are
// independent of one another, so each worker writes its own index and the
// trees and diagnostics are merged in file order, which keeps the result
// identical to a serial parse.
func parseFiles(files []*source.File, diags []diag.Diagnostic) ([]*ast.File, []diag.Diagnostic) {
	if len(files) == 0 {
		return nil, diags
	}
	trees := make([]*ast.File, len(files))
	perFile := make([][]diag.Diagnostic, len(files))

	workers := runtime.GOMAXPROCS(0)
	if workers > len(files) {
		workers = len(files)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				tree, fileDiags := parser.Parse(files[index])
				trees[index] = tree
				perFile[index] = fileDiags
			}
		}()
	}
	for index := range files {
		jobs <- index
	}
	close(jobs)
	wg.Wait()

	for _, fileDiags := range perFile {
		diags = append(diags, fileDiags...)
	}
	return trees, diags
}
