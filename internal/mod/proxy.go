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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
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

// HashTree returns the hash of a module's content computed over a directory
// rather than an archive. The names are the ones an archive would use, so the
// same module fetched from a repository and from a proxy hash alike, and a
// checksum file records one hash for both.
func HashTree(path, version, dir string) (string, error) {
	var names []string
	err := filepath.WalkDir(dir, func(entry string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, entry)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		// A repository keeps its own history beside the files, which is not
		// part of the module.
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if info.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		names = append(names, path+"@"+version+"/"+rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	summary := sha256.New()
	for _, name := range names {
		rel := strings.TrimPrefix(name, path+"@"+version+"/")
		content := sha256.New()
		file, err := os.Open(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		_, err = io.Copy(content, file)
		file.Close()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(summary, "%x  %s\n", content.Sum(nil), name)
	}
	return hashOf(summary.Sum(nil)), nil
}

// repoAddress returns where the repository of a module path is. A server that
// serves the module answers the query with a meta tag naming the repository,
// which is how a module whose path is not its repository root is served; a
// module whose path is its repository root answers nothing, and the path is
// the address. The query is tried over https first and over http second: the
// content is checked against the checksum file either way, so a plain
// connection cannot change what a build reads, only whether it can read it.
func (f *Fetcher) repoAddress(path string) string {
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	for _, scheme := range []string{"https", "http"} {
		response, err := client.Get(scheme + "://" + path + "?zyx-get=1")
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			continue
		}
		if repo, ok := importMeta(string(body), path); ok {
			return repo
		}
	}
	return "https://" + path
}

// metaTag matches one meta tag of a page, which is all of the HTML a module
// path lookup reads.
var metaTag = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)

// nameAttr and contentAttr read the two attributes a meta tag carries.
var (
	nameAttr    = regexp.MustCompile(`(?is)\bname\s*=\s*"([^"]*)"`)
	contentAttr = regexp.MustCompile(`(?is)\bcontent\s*=\s*"([^"]*)"`)
)

// importMeta returns the repository a page names for a module path, which is
// what a server writes in a zyx-import meta tag:
//
//	<meta name="zyx-import" content="example.com/dep git https://example.com/dep.git">
func importMeta(body, path string) (string, bool) {
	for _, tag := range metaTag.FindAllString(body, -1) {
		name := nameAttr.FindStringSubmatch(tag)
		content := contentAttr.FindStringSubmatch(tag)
		if len(name) < 2 || len(content) < 2 || !strings.EqualFold(name[1], "zyx-import") {
			continue
		}
		fields := strings.Fields(content[1])
		if len(fields) != 3 || fields[0] != path || fields[1] != "git" {
			continue
		}
		return fields[2], true
	}
	return "", false
}

// FetchDirect reads a module version straight from the version control
// system, which is what a source list naming direct asks for. The repository
// is the module path itself, which is where a module whose path is its
// repository root lives, and the version names a tag in it. The hashes are
// checked against what the checksum file recorded before anything is stored,
// so a repository and a proxy are held to the same standard.
func (f *Fetcher) FetchDirect(cache *ModuleCache, path, version, wantContent, wantManifest string) (content, manifest string, err error) {
	if cache == nil {
		return "", "", fmt.Errorf("module %s %s: there is no cache to store it in", path, version)
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return "", "", fmt.Errorf("module %s %s: fetching from a repository needs git", path, version)
	}
	work, err := os.MkdirTemp("", "zyx-vcs-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(work)

	// The repository is the module path, with or without the suffix a server
	// writes for one. A clone of one tag is enough: a module version is
	// immutable, so no history is needed.
	checkout := filepath.Join(work, "repo")
	address := f.repoAddress(path)
	clone := func(target string) error {
		return runGit(git, work, "clone", "--quiet", "--depth", "1",
			"--branch", version, "--single-branch", target, checkout)
	}
	if err := clone(address); err != nil {
		if err := clone(address + ".git"); err != nil {
			return "", "", fmt.Errorf("module %s %s: no repository serves it: %w", path, version, err)
		}
	}
	if _, err := os.Stat(filepath.Join(checkout, "zyx.mod")); err != nil {
		return "", "", fmt.Errorf("module %s %s: the repository holds no zyx.mod at its root", path, version)
	}
	content, err = HashTree(path, version, checkout)
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(filepath.Join(checkout, "zyx.mod"))
	if err != nil {
		return "", "", err
	}
	manifest = HashManifest(path, version, data)
	if wantContent != "" && content != wantContent {
		return "", "", fmt.Errorf("module %s %s: the repository content hash is %s, and zyx.sum records %s", path, version, content, wantContent)
	}
	if wantManifest != "" && manifest != wantManifest {
		return "", "", fmt.Errorf("module %s %s: the repository manifest hash is %s, and zyx.sum records %s", path, version, manifest, wantManifest)
	}
	if err := cache.PutTree(path, version, content, checkout); err != nil {
		return "", "", err
	}
	return content, manifest, nil
}

// directVersions returns the tags a repository holds for a module, which is
// what a version query asks of a repository rather than of a proxy.
func (f *Fetcher) directVersions(path string) ([]string, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("module %s: fetching from a repository needs git", path)
	}
	out, err := runGitOutput(git, "ls-remote", "--tags", f.repoAddress(path))
	if err != nil {
		return nil, err
	}
	var versions []string
	for _, line := range strings.Split(string(out), "\n") {
		_, ref, ok := strings.Cut(line, "refs/tags/")
		if !ok {
			continue
		}
		// An annotated tag names the commit it points at on a second line,
		// and the tag itself is what a version is.
		if ref = strings.TrimSuffix(strings.TrimSpace(ref), "^{}"); ref != "" {
			versions = append(versions, ref)
		}
	}
	return versions, nil
}

// runGit runs one git command in a directory, answering what it said when it
// fails.
func runGit(git, dir string, args ...string) error {
	cmd := exec.Command(git, args...)
	cmd.Dir = dir
	var message bytes.Buffer
	cmd.Stderr = &message
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s", gitMessage(&message, err))
	}
	return nil
}

// runGitOutput runs one git command and answers what it wrote.
func runGitOutput(git string, args ...string) ([]byte, error) {
	cmd := exec.Command(git, args...)
	var message bytes.Buffer
	cmd.Stderr = &message
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s", gitMessage(&message, err))
	}
	return out, nil
}

// gitMessage returns what a git command said, or the error when it said
// nothing.
func gitMessage(message *bytes.Buffer, err error) string {
	if text := strings.TrimSpace(message.String()); text != "" {
		return text
	}
	return err.Error()
}

// PutTree stores a module version from a directory rather than an archive,
// which is what a fetch from a repository has. The content hash is checked
// against what the checksum file recorded, and the tree is moved into place,
// so a reader sees either nothing or the whole module.
func (c *ModuleCache) PutTree(path, version, content, dir string) error {
	got, err := HashTree(path, version, dir)
	if err != nil {
		return err
	}
	if content != "" && got != content {
		return fmt.Errorf("module %s %s: content hash is %s, and zyx.sum records %s", path, version, got, content)
	}
	dest := c.Dir(path, version)
	temp, err := os.MkdirTemp(c.root, ".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	if err := copyTreeInto(dir, temp); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(temp, "zyx.mod")); err != nil {
		return fmt.Errorf("module %s %s: the tree holds no zyx.mod", path, version)
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.Rename(temp, dest)
}

// copyTreeInto copies the files of a directory into another, without the
// history a repository keeps beside them.
func copyTreeInto(from, to string) error {
	return filepath.WalkDir(from, func(entry string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, entry)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" || strings.HasPrefix(filepath.ToSlash(rel), ".git/") {
			if info.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(entry)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
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
func (f *Fetcher) eachSource(path, version string, use func(Source) error) error {
	if MatchPatterns(f.Private, path) || MatchPatterns(f.NoProxy, path) {
		return fmt.Errorf("module %s is private, so it is not fetched from a source", path)
	}
	var last error
	for _, source := range f.Sources {
		if source.Off {
			return fmt.Errorf("module fetching is off")
		}
		err := use(source)
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
	err := f.eachSource(path, "latest", func(source Source) error {
		var versions []string
		var err error
		if source.Direct {
			versions, err = f.directVersions(path)
		} else {
			versions, err = (&Proxy{Base: source.Proxy, Client: f.Client}).Versions(path)
		}
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
// than trusted: what a source serves is only usable once something has fixed
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
	return f.eachSource(path, version, func(source Source) error {
		if source.Direct {
			_, _, err := f.FetchDirect(cache, path, version, wantContent, wantManifest)
			return err
		}
		proxy := &Proxy{Base: source.Proxy, Client: f.Client}
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
	err = f.eachSource(path, version, func(source Source) error {
		if source.Direct {
			var err error
			content, manifest, err = f.FetchDirect(cache, path, version, "", "")
			return err
		}
		proxy := &Proxy{Base: source.Proxy, Client: f.Client}
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
