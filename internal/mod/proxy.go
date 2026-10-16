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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
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

// A Proxy is one source of modules, speaking the proxy protocol go uses: a
// base URL serves the versions of a module, the metadata of one version, its
// manifest, and its archive.
type Proxy struct {
	// Base is the URL the protocol paths are appended to.
	Base string
	// Client carries out the requests.
	Client *http.Client
}

// Info is what a proxy reports about one version.
type Info struct {
	Version string
	Time    time.Time
}

// url returns the address of one protocol path.
func (p *Proxy) url(path, suffix string) string {
	return strings.TrimSuffix(p.Base, "/") + "/" + escapePath(path) + "/@v/" + escapePath(suffix)
}

// ErrNotThere reports that a source does not have what was asked for, which
// is what lets a source list move on to the next entry.
var ErrNotThere = errors.New("the source does not have the module")

// get reads one protocol path.
func (p *Proxy) get(address string) ([]byte, error) {
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Get(address)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return nil, fmt.Errorf("%s: %w", address, ErrNotThere)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", address, response.Status)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// Versions returns the versions a proxy lists for a module.
func (p *Proxy) Versions(path string) ([]string, error) {
	body, err := p.get(p.url(path, "list"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(body), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out, nil
}

// InfoOf returns what a proxy reports about one version.
func (p *Proxy) InfoOf(path, version string) (Info, error) {
	body, err := p.get(p.url(path, version+".info"))
	if err != nil {
		return Info{}, err
	}
	var raw struct {
		Version string
		Time    time.Time
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Info{}, fmt.Errorf("%s %s: the metadata is not readable: %w", path, version, err)
	}
	return Info{Version: raw.Version, Time: raw.Time}, nil
}

// Manifest returns the manifest a proxy serves for one version.
func (p *Proxy) Manifest(path, version string) ([]byte, error) {
	return p.get(p.url(path, version+".mod"))
}

// Archive returns the archive a proxy serves for one version.
func (p *Proxy) Archive(path, version string) ([]byte, error) {
	return p.get(p.url(path, version+".zip"))
}

// A Fetcher reads modules from the sources the environment names. The list is
// the one ZYXPROXY holds: proxies in the order to try them, and the words off
// and direct. A comma moves on to the next source when one answers that it
// does not have the module; a pipe moves on whatever went wrong.
type Fetcher struct {
	// Sources is the source list, in the order to try them.
	Sources []Source
	// Client carries out the requests.
	Client *http.Client
	// NoProxy and Private hold the path patterns whose modules bypass the
	// proxies and the checksum database.
	NoProxy []string
	Private []string
}

// A Source is one entry of the source list.
type Source struct {
	// Proxy is the source to ask, empty for the keywords.
	Proxy string
	// Off refuses to fetch, and Direct reaches the version control system.
	Off    bool
	Direct bool
	// FallbackOnAny moves to the next source whatever went wrong, which is
	// what a pipe between entries means.
	FallbackOnAny bool
}

// ParseSources reads a source list, the way ZYXPROXY writes it: proxies in
// the order to try them, separated by commas, and the words off and direct. A
// pipe in place of a comma moves on to the next source whatever went wrong,
// where a comma moves on only when a source answers that it does not have the
// module.
func ParseSources(list string) ([]Source, error) {
	if strings.TrimSpace(list) == "" {
		return nil, nil
	}
	var out []Source
	for _, part := range splitSources(list) {
		text := strings.TrimSpace(part.text)
		if text == "" {
			continue
		}
		source := Source{FallbackOnAny: part.pipe}
		switch text {
		case "off":
			source.Off = true
		case "direct":
			source.Direct = true
		default:
			if !strings.Contains(text, "://") {
				return nil, fmt.Errorf("ZYXPROXY entry %q is neither a URL nor off or direct", text)
			}
			source.Proxy = text
		}
		out = append(out, source)
	}
	return out, nil
}

// splitSource is one entry of the source list with the separator that follows
// it.
type splitSource struct {
	text string
	pipe bool
}

// splitSources splits a source list, keeping how each entry is separated from
// the next.
func splitSources(list string) []splitSource {
	var out []splitSource
	var b strings.Builder
	for _, r := range list {
		switch r {
		case ',', '|':
			out = append(out, splitSource{text: b.String(), pipe: r == '|'})
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	out = append(out, splitSource{text: b.String()})
	return out
}

// MatchPatterns reports whether a path matches one of the patterns a
// comma-separated list holds, which is the rule ZYXNOPROXY and ZYXPRIVATE
// state. A pattern with N path elements matches the first N elements of the
// path, so example.com/private matches example.com/private/sub, and a pattern
// with a wildcard matches those elements the way a path pattern does.
func MatchPatterns(patterns []string, importPath string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSuffix(strings.TrimSpace(pattern), "/")
		if pattern == "" {
			continue
		}
		// The pattern is matched against as many elements of the path as it
		// has itself, so a wildcard never has to cross a separator.
		elements := strings.Count(pattern, "/")
		prefix := importPath
		for i := 0; i < len(importPath); i++ {
			if importPath[i] != '/' {
				continue
			}
			if elements == 0 {
				prefix = importPath[:i]
				break
			}
			elements--
		}
		if elements > 0 {
			continue
		}
		if ok, err := path.Match(pattern, prefix); err == nil && ok {
			return true
		}
	}
	return false
}

// ParsePatterns reads a comma-separated pattern list.
func ParsePatterns(list string) []string {
	var out []string
	for _, pattern := range strings.Split(list, ",") {
		if pattern = strings.TrimSpace(pattern); pattern != "" {
			out = append(out, pattern)
		}
	}
	return out
}

// eachSource asks each source in turn for a module version, moving on the way
// the list says to. It answers the error of the last source when none served
// it.
func (f *Fetcher) eachSource(path, version string, use func(*Proxy) error) error {
	if MatchPatterns(f.Private, path) || MatchPatterns(f.NoProxy, path) {
		return fmt.Errorf("module %s is private, so it is not fetched from a proxy", path)
	}
	var last error
	for _, source := range f.Sources {
		if source.Off {
			return fmt.Errorf("module fetching is off")
		}
		if source.Direct {
			last = fmt.Errorf("module %s %s: fetching straight from the version control system is not implemented", path, version)
			if source.FallbackOnAny {
				continue
			}
			return last
		}
		err := use(&Proxy{Base: source.Proxy, Client: f.Client})
		if err == nil {
			return nil
		}
		last = err
		if source.FallbackOnAny || errors.Is(err, ErrNotThere) {
			continue
		}
		return err
	}
	if last == nil {
		last = fmt.Errorf("module %s %s: no source is configured to fetch it", path, version)
	}
	return last
}

// Latest returns the highest release a source lists for a module.
func (f *Fetcher) Latest(path string) (string, error) {
	latest := ""
	err := f.eachSource(path, "latest", func(proxy *Proxy) error {
		versions, err := proxy.Versions(path)
		if err != nil {
			return err
		}
		version, err := LatestStable(versions)
		if err != nil {
			return err
		}
		latest = version
		return nil
	})
	return latest, err
}

// FetchInto reads a module version from the first source that serves it,
// checks it against the hashes the checksum file records, and stores it in
// the cache. A module whose hashes the file does not record is refused rather
// than trusted: what a proxy serves is only usable once something has fixed
// it.
func (f *Fetcher) FetchInto(cache *ModuleCache, sum *Sum, path, version string) error {
	if cache == nil {
		return fmt.Errorf("module %s %s: there is no cache to store it in", path, version)
	}
	wantContent := ""
	wantManifest := ""
	if sum != nil {
		wantContent = sum.Content(path, version)
		wantManifest = sum.Manifest(path, version)
	}
	if wantContent == "" || wantManifest == "" {
		return fmt.Errorf("module %s %s has no recorded hash in zyx.sum", path, version)
	}
	return f.eachSource(path, version, func(proxy *Proxy) error {
		manifest, err := proxy.Manifest(path, version)
		if err != nil {
			return err
		}
		if got := HashManifest(path, version, manifest); got != wantManifest {
			return fmt.Errorf("module %s %s: the manifest hash is %s, and zyx.sum records %s", path, version, got, wantManifest)
		}
		archive, err := proxy.Archive(path, version)
		if err != nil {
			return err
		}
		return cache.Put(path, version, wantContent, archive)
	})
}

// FetchNew reads a module version from the first source that serves it and
// stores it, answering the two hashes a checksum file records for it. It is
// what a fetch that has nothing to check against uses: the hashes it answers
// with are the ones every later fetch is checked against.
func (f *Fetcher) FetchNew(cache *ModuleCache, path, version string) (content, manifest string, err error) {
	if cache == nil {
		return "", "", fmt.Errorf("module %s %s: there is no cache to store it in", path, version)
	}
	err = f.eachSource(path, version, func(proxy *Proxy) error {
		served, err := proxy.Manifest(path, version)
		if err != nil {
			return err
		}
		archive, err := proxy.Archive(path, version)
		if err != nil {
			return err
		}
		content, err = HashContent(archive)
		if err != nil {
			return err
		}
		manifest = HashManifest(path, version, served)
		if err := cache.Put(path, version, content, archive); err != nil {
			return err
		}
		// The manifest a source served has to be the one inside the archive it
		// served, or the two halves of what a build reads would disagree.
		stored, err := cache.ManifestHash(path, version)
		if err != nil {
			return err
		}
		if stored != manifest {
			return fmt.Errorf("module %s %s: the manifest a source served is not the one in the archive", path, version)
		}
		return nil
	})
	return content, manifest, err
}
