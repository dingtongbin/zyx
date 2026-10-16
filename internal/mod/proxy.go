// The module cache: where fetched modules live.
//
// A module version is stored under its path and version, with its manifest
// and its tree, and the content hash it was stored under is the one zyx.sum
// recorded. The cache is what makes a build repeatable: once a version is
// there, the build reads it rather than the network, and what it reads was
// checked against the hash the module's own checksum file fixed.

package mod

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// A ModuleCache is a directory of fetched modules.
type ModuleCache struct {
	root string
}

// OpenModuleCache prepares a cache directory.
func OpenModuleCache(root string) (*ModuleCache, error) {
	if root == "" {
		return nil, fmt.Errorf("module cache: no directory")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("module cache: %w", err)
	}
	return &ModuleCache{root: root}, nil
}

// Root returns the directory the cache lives in.
func (c *ModuleCache) Root() string { return c.root }

// Dir returns where a module version's tree lives.
func (c *ModuleCache) Dir(path, version string) string {
	return filepath.Join(c.root, escapePath(path)+"@"+version)
}

// Has reports whether a module version is in the cache.
func (c *ModuleCache) Has(path, version string) bool {
	info, err := os.Stat(filepath.Join(c.Dir(path, version), "zyx.mod"))
	return err == nil && !info.IsDir()
}

// Manifest returns the manifest of a module version from the cache, which is
// what fills a version-selection graph without the network.
func (c *ModuleCache) Manifest(path, version string) (*File, error) {
	data, err := os.ReadFile(filepath.Join(c.Dir(path, version), "zyx.mod"))
	if err != nil {
		return nil, fmt.Errorf("module %s %s is not in the cache: %w", path, version, err)
	}
	return Parse(data)
}

// ManifestHash returns the hash a cached module's manifest has, so a caller
// can check it against the checksum file.
func (c *ModuleCache) ManifestHash(path, version string) (string, error) {
	data, err := os.ReadFile(filepath.Join(c.Dir(path, version), "zyx.mod"))
	if err != nil {
		return "", err
	}
	return HashManifest(path, version, data), nil
}

// Put stores a module version. The archive is checked against the content
// hash the checksum file recorded, unpacked into a directory of its own, and
// moved into place, so a reader sees either nothing or the whole module.
func (c *ModuleCache) Put(path, version, content string, archive []byte) error {
	got, err := HashContent(archive)
	if err != nil {
		return err
	}
	if content != "" && got != content {
		return fmt.Errorf("module %s %s: content hash is %s, and zyx.sum records %s", path, version, got, content)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	dir := c.Dir(path, version)
	temp, err := os.MkdirTemp(c.root, ".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	for _, file := range reader.File {
		name := unpackName(path, version, file.Name)
		if name == "" {
			return fmt.Errorf("module %s %s: the archive holds %q, which is outside the module", path, version, file.Name)
		}
		target := filepath.Join(temp, filepath.FromSlash(name))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := unpackFile(target, file); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(temp, "zyx.mod")); err != nil {
		return fmt.Errorf("module %s %s: the archive holds no zyx.mod", path, version)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	return os.Rename(temp, dir)
}

// unpackName returns where a file of a module archive goes inside the module
// directory, or the empty string when it names something outside it. An
// archive names its files `<path>@<version>/<relative>`, and a path that
// climbs out of the module is refused rather than written.
func unpackName(path, version, name string) string {
	prefix := path + "@" + version + "/"
	if !strings.HasPrefix(name, prefix) {
		return ""
	}
	rel := strings.TrimPrefix(name, prefix)
	if rel == "" {
		return ""
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return ""
	}
	return clean
}

// unpackFile writes one file of a module archive.
func unpackFile(target string, file *zip.File) error {
	source, err := file.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	info := file.FileInfo()
	dest, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dest, source); err != nil {
		dest.Close()
		return err
	}
	return dest.Close()
}

// escapePath spells a module path as one directory name. An uppercase letter
// becomes an exclamation mark and the lowercase letter, so two paths that
// differ only in case stay two directories on a file system that does not
// tell them apart.
func escapePath(path string) string {
	var b strings.Builder
	for _, r := range path {
		if unicode.IsUpper(r) {
			b.WriteByte('!')
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
