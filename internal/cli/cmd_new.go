// The new command scaffolds a project.

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// newProject writes a module skeleton into a directory.
func (s *Session) newProject(args []string) int {
	o, err := s.parseOptions(args)
	if err != nil {
		return s.usageError(err)
	}
	if len(o.paths) != 1 {
		return s.usageError(fmt.Errorf("new takes one directory"))
	}
	dir := o.paths[0]
	name := filepath.Base(filepath.Clean(dir))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return s.usageError(fmt.Errorf("%s is not a project directory", dir))
	}
	modulePath := dir
	if !strings.Contains(modulePath, ".") {
		modulePath = "example.com/" + name
	}
	files := map[string]string{
		"zyx.mod":    fmt.Sprintf("module %s\n\nzyx 0.1\n", modulePath),
		"main.zyx":   "package main\n\nimport \"std/fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello, world\")\n}\n",
		".gitignore": "build/\n.zyx-cache/\n.zyx-mod/\n",
	}
	for name, body := range files {
		full := filepath.Join(dir, name)
		if _, err := os.Stat(full); err == nil {
			fmt.Fprintf(s.Stderr, "zyx: %s already exists\n", full)
			return exitError
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
	}
	return exitOK
}
