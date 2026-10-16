// The checksum file: the hashes a build locates its dependencies by.
//
// A zyx.sum records two hashes for each module version, one over the module's
// content and one over its manifest, so that a dependency is fixed by hash
// rather than by trusting whatever a proxy served. The form is the one go.sum
// uses: a line per hash, `<path> <version> h1:<hash>`, with the manifest line
// marked by the `/zyx.mod` suffix on the version.

package mod

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"sort"
	"strings"
)

// A Sum is the parsed zyx.sum, held in the order it is written in.
type Sum struct {
	lines []sumLine
}

// sumLine is one recorded hash.
type sumLine struct {
	// Path is the module path.
	Path string
	// Version is the version, without the manifest suffix.
	Version string
	// Manifest marks the line that records the manifest rather than the
	// module's content.
	Manifest bool
	// Hash is the recorded hash, in the h1: form.
	Hash string
}

// manifestSuffix marks the line that records a module's manifest.
const manifestSuffix = "/zyx.mod"

// ParseSum reads a checksum file. A line that is not a recorded hash is an
// error: a checksum file that cannot be read cannot be trusted.
func ParseSum(data []byte) (*Sum, error) {
	s := &Sum{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("zyx.sum line %d is not a recorded hash: %q", i+1, line)
		}
		entry := sumLine{Path: fields[0], Version: fields[1], Hash: fields[2]}
		if !strings.HasPrefix(entry.Hash, "h1:") {
			return nil, fmt.Errorf("zyx.sum line %d is not an h1 hash: %q", i+1, line)
		}
		if strings.HasSuffix(entry.Version, manifestSuffix) {
			entry.Manifest = true
			entry.Version = strings.TrimSuffix(entry.Version, manifestSuffix)
		}
		s.lines = append(s.lines, entry)
	}
	return s, nil
}

// Format returns the file text: one hash per line, in byte order, so a
// rewrite is deterministic and a diff is readable.
func (s *Sum) Format() []byte {
	lines := append([]sumLine(nil), s.lines...)
	sort.Slice(lines, func(i, j int) bool { return lineKey(lines[i]) < lineKey(lines[j]) })
	var b strings.Builder
	for _, line := range lines {
		fmt.Fprintf(&b, "%s %s%s %s\n", line.Path, line.Version, manifestSuffixOf(line), line.Hash)
	}
	return []byte(b.String())
}

// manifestSuffixOf returns the suffix that marks a manifest line.
func manifestSuffixOf(line sumLine) string {
	if line.Manifest {
		return manifestSuffix
	}
	return ""
}

// lineKey orders the lines of the file.
func lineKey(line sumLine) string {
	return line.Path + " " + line.Version + manifestSuffixOf(line)
}

// Content returns the recorded hash of a module's content, or the empty
// string when the file records none.
func (s *Sum) Content(path, version string) string { return s.find(path, version, false) }

// Manifest returns the recorded hash of a module's manifest, or the empty
// string when the file records none.
func (s *Sum) Manifest(path, version string) string { return s.find(path, version, true) }

// find returns one recorded hash.
func (s *Sum) find(path, version string, manifest bool) string {
	for _, line := range s.lines {
		if line.Path == path && line.Version == version && line.Manifest == manifest {
			return line.Hash
		}
	}
	return ""
}

// Add records both hashes of a module version, replacing what the file held
// for it.
func (s *Sum) Add(path, version, content, manifest string) {
	s.Remove(path, version)
	if content != "" {
		s.lines = append(s.lines, sumLine{Path: path, Version: version, Hash: content})
	}
	if manifest != "" {
		s.lines = append(s.lines, sumLine{Path: path, Version: version, Manifest: true, Hash: manifest})
	}
}

// Remove drops both hashes of a module version.
func (s *Sum) Remove(path, version string) {
	kept := s.lines[:0]
	for _, line := range s.lines {
		if line.Path == path && line.Version == version {
			continue
		}
		kept = append(kept, line)
	}
	s.lines = kept
}

// Prune drops every hash of a module the keep function does not name, which
// is what tidy does with the requirements that are no longer reachable.
func (s *Sum) Prune(keep func(path string) bool) {
	kept := s.lines[:0]
	for _, line := range s.lines {
		if keep(line.Path) {
			kept = append(kept, line)
		}
	}
	s.lines = kept
}

// Paths returns the module paths the file records, in byte order.
func (s *Sum) Paths() []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range s.lines {
		if !seen[line.Path] {
			seen[line.Path] = true
			out = append(out, line.Path)
		}
	}
	sort.Strings(out)
	return out
}

// Empty reports whether the file records nothing.
func (s *Sum) Empty() bool { return len(s.lines) == 0 }

// HashContent returns the hash of a module's content, which is what a proxy
// serves as a zip. The hash covers the name and the content hash of every
// file, in name order, so it fixes the whole tree rather than one file.
func HashContent(zipData []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return "", fmt.Errorf("the module content is not a zip: %w", err)
	}
	files := make([]string, 0, len(reader.File))
	byName := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		files = append(files, file.Name)
		byName[file.Name] = file
	}
	sort.Strings(files)
	summary := sha256.New()
	for _, name := range files {
		if strings.Contains(name, "\n") {
			return "", fmt.Errorf("the module content holds a file name with a line break: %q", name)
		}
		file := byName[name]
		if file.FileInfo().IsDir() {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			return "", err
		}
		content := sha256.New()
		_, err = io.Copy(content, opened)
		opened.Close()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(summary, "%x  %s\n", content.Sum(nil), name)
	}
	return hashOf(summary.Sum(nil)), nil
}

// HashManifest returns the hash of a module's manifest, named the way a proxy
// names it inside the module it belongs to.
func HashManifest(path, version string, data []byte) string {
	summary := sha256.New()
	content := sha256.Sum256(data)
	fmt.Fprintf(summary, "%x  %s@%s/zyx.mod\n", content, path, version)
	return hashOf(summary.Sum(nil))
}

// hashOf spells a summary the way the file records it.
func hashOf(sum []byte) string {
	return "h1:" + base64.StdEncoding.EncodeToString(sum)
}
