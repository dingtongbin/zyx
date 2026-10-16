// The check command runs front-end analysis without emitting object code.

package cli

import (
	"bytes"
	"fmt"
	"os"

	"zyx/internal/build"
	"zyx/internal/diag"
	zyxfmt "zyx/internal/fmt"
)

// check analyzes every target package and reports the diagnostics.
func (s *Session) check(args []string) int {
	o, err := s.parseOptions(args)
	if err != nil {
		return s.usageError(err)
	}
	m, err := s.findModule(s.Dir)
	if err != nil {
		return s.usageError(err)
	}
	if !s.prepareModule(m) {
		return exitError
	}
	paths, err := s.targets(m, o)
	if err != nil {
		return s.usageError(err)
	}
	session := &build.Session{Loader: s.loader(m, o)}
	reported := map[string]bool{}
	formatted := map[string]bool{}
	failed := false
	for _, path := range paths {
		results, err := session.FrontEnd(path)
		if err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		if s.reportOnce(results, reported) {
			failed = true
		}
		if s.reportUnformatted(results, paths, formatted) {
			failed = true
		}
	}
	if failed {
		return exitError
	}
	return exitOK
}

// reportUnformatted reports every file of a target package that is not in
// the canonical form `zyx fmt` writes, so a check refuses text a formatting
// run would change. A dependency's text is not the target's to change.
func (s *Session) reportUnformatted(results []*build.Result, paths []string, reported map[string]bool) bool {
	targets := map[string]bool{}
	for _, path := range paths {
		targets[path] = true
	}
	failed := false
	for _, res := range results {
		if !targets[res.Package.ImportPath] || reported[res.Package.ImportPath] {
			continue
		}
		reported[res.Package.ImportPath] = true
		for _, file := range res.Package.Files {
			if bytes.Equal(zyxfmt.Source(file), file.Data()) {
				continue
			}
			failed = true
			fmt.Fprintf(s.Stderr, "%s:1:1: %s\n    help: run zyx fmt\n",
				file.Name(), diag.CodeUnformatted)
		}
	}
	return failed
}

// reportOnce prints the diagnostics of the packages that have not been
// reported yet, so that a run over several targets reports a shared
// dependency once. It reports whether any diagnostic is an error.
func (s *Session) reportOnce(results []*build.Result, reported map[string]bool) bool {
	failed := false
	for _, res := range results {
		if reported[res.Package.ImportPath] {
			continue
		}
		reported[res.Package.ImportPath] = true
		if s.report(res.Diags) {
			failed = true
		}
	}
	return failed
}

// report prints diagnostics and reports whether any is an error.
func (s *Session) report(diags []diag.Diagnostic) bool {
	failed := false
	for _, d := range diags {
		fmt.Fprint(s.Stderr, d.RenderString())
		failed = true
	}
	return failed
}

// usageError prints a usage error and returns its exit code.
func (s *Session) usageError(err error) int {
	fmt.Fprintln(s.Stderr, "zyx:", err)
	return exitUsage
}

// writeOutput writes a command's output to a file or to standard output.
func (s *Session) writeOutput(path string, data []byte) error {
	if path == "" {
		_, err := s.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
