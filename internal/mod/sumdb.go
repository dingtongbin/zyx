// The checksum database: a transparency log of the hashes a build fixes its
// dependencies by.
//
// The database is a Merkle tree whose head is signed by a key the environment
// names. A record is authenticated by proving that it is in the tree the
// signed head describes, and the head is authenticated against the head the
// client saw before, so a database cannot quietly rewrite what it said. The
// pieces here are the two ends of that: the note, which is how a head is
// signed and verified, and the log hashing, which is what the proofs are made
// of.

package mod

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"math/bits"
	"net/http"
	"strconv"
	"strings"
)

// A SumDB is a checksum database a build consults.
type SumDB struct {
	// Name is the database's name, which its key is named by.
	Name string
	// Key is the public key the database signs with.
	Key ed25519.PublicKey
	// KeyHash is the four bytes a note carries to name that key.
	KeyHash []byte
	// Base is the URL the protocol paths are appended to.
	Base string
}

// signatureDash is the character a note's signature lines begin with, which is
// an em dash.
const signatureDash = "\u2014"

// ParseSumDB reads the ZYXSUMDB setting: the database's name, optionally its
// public key after a plus, and optionally its URL. The word off turns the
// database off, which leaves a build to trust the checksum file alone.
func ParseSumDB(setting string) (*SumDB, error) {
	setting = strings.TrimSpace(setting)
	if setting == "" || setting == "off" {
		return nil, nil
	}
	name, rest := setting, ""
	if i := strings.IndexAny(setting, " \t"); i >= 0 {
		name, rest = setting[:i], strings.TrimSpace(setting[i+1:])
	}
	key := ""
	if i := strings.IndexByte(name, '+'); i >= 0 {
		name, key = name[:i], name[i+1:]
	}
	if name == "" {
		return nil, fmt.Errorf("ZYXSUMDB names no database")
	}
	db := &SumDB{Name: name, Base: "https://" + name}
	if rest != "" {
		db.Base = rest
	}
	if key == "" {
		return nil, fmt.Errorf("checksum database %s: ZYXSUMDB names no public key, and this build knows none", name)
	}
	parsed, hash, err := ParseKey(name, key)
	if err != nil {
		return nil, err
	}
	db.Key, db.KeyHash = parsed, hash
	return db, nil
}

// ParseKey reads a public key in the form a note names one: the database's
// name, a plus, and the base64 of a four byte hash of the name and the key
// followed by the key itself. The hash is what a signature carries, so a
// reader can tell which key signed without being told.
func ParseKey(name, encoded string) (ed25519.PublicKey, []byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, nil, fmt.Errorf("checksum database %s: the public key is not base64: %w", name, err)
	}
	if len(raw) != 4+ed25519.PublicKeySize {
		return nil, nil, fmt.Errorf("checksum database %s: the public key is %d bytes, want %d", name, len(raw), 4+ed25519.PublicKeySize)
	}
	key, hash := ed25519.PublicKey(raw[4:]), raw[:4]
	if want := keyHash(name, key); !bytes.Equal(want, hash) {
		return nil, nil, fmt.Errorf("checksum database %s: the public key does not match the hash it carries", name)
	}
	return key, hash, nil
}

// keyHash returns the four bytes that name a key: the start of the hash of the
// database's name and the key.
func keyHash(name string, key ed25519.PublicKey) []byte {
	sum := sha256.Sum256(append([]byte(name+"\n"), key...))
	return sum[:4]
}

// EncodeKey spells a public key the way a note names one, which is what a
// database publishes and what ZYXSUMDB is given.
func EncodeKey(name string, key ed25519.PublicKey) string {
	raw := append(keyHash(name, key), key...)
	return base64.StdEncoding.EncodeToString(raw)
}

// SplitNote returns the text a note signs and the signature lines that follow
// it. The text is everything up to the blank line that separates the two, and
// it is what the signature covers.
func SplitNote(data []byte) (text []byte, signatures []string, err error) {
	i := bytes.Index(data, []byte("\n\n"))
	if i < 0 {
		return nil, nil, fmt.Errorf("note: the message carries no signature")
	}
	text = data[:i+1]
	for _, line := range strings.Split(string(data[i+2:]), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			signatures = append(signatures, line)
		}
	}
	return text, signatures, nil
}

// VerifyNote checks that a note's text is signed by a key, and answers the
// text. A note signed by no key the caller knows is refused: the whole point
// of the signature is that the text came from the database and not from
// whatever served it.
func VerifyNote(data []byte, db *SumDB) (string, error) {
	if db == nil || db.Key == nil {
		return "", fmt.Errorf("note: no key is known for the database")
	}
	text, signatures, err := SplitNote(data)
	if err != nil {
		return "", err
	}
	for _, line := range signatures {
		fields := strings.SplitN(line, " ", 3)
		if len(fields) != 3 || fields[0] != signatureDash || fields[1] != db.Name {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(fields[2])
		if err != nil || len(raw) != 4+ed25519.SignatureSize {
			continue
		}
		if !bytes.Equal(raw[:4], db.KeyHash) {
			continue
		}
		if ed25519.Verify(db.Key, text, raw[4:]) {
			return string(text), nil
		}
	}
	return "", fmt.Errorf("note: no signature by %s verifies the message", db.Name)
}

// SignNote signs a text the way a database does, which is what lets a test
// hold the reader to a writer it made itself.
func SignNote(text string, db *SumDB, key ed25519.PrivateKey) []byte {
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	signature := ed25519.Sign(key, []byte(text))
	raw := append(append([]byte(nil), db.KeyHash...), signature...)
	return []byte(text + "\n" + signatureDash + " " + db.Name + " " +
		base64.StdEncoding.EncodeToString(raw) + "\n")
}

// logTileHeight is the height of the tiles a log serves, which the protocol
// fixes at 8: a tile holds up to 256 hashes.
const logTileHeight = 8

// A logClient reads a transparency log over the protocol: the hashes come
// from tiles, and a record comes from the lookup endpoint.
type logClient struct {
	db     *SumDB
	client *http.Client
	// size is the size of the log being read, which tells a proof how many
	// hashes a level holds.
	size int64
	// tiles holds the tiles already read, so a proof asks for each once.
	tiles map[string][]byte
}

// newLogClient returns a client for a database.
func newLogClient(db *SumDB, client *http.Client) *logClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &logClient{db: db, client: client, tiles: map[string][]byte{}}
}

// get reads one protocol path of the database.
func (l *logClient) get(suffix string) ([]byte, error) {
	address := strings.TrimSuffix(l.db.Base, "/") + "/" + suffix
	response, err := l.client.Get(address)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", address, response.Status)
	}
	return io.ReadAll(response.Body)
}

// tile returns the hashes of one tile. A tile whose last hashes are past the
// end of the log is asked for at the width the log has, and a server that
// answers nothing for it is asked for the whole tile instead.
func (l *logClient) tile(level, index, width int64) ([]byte, error) {
	path := fmt.Sprintf("tile/%d/%d/%d", logTileHeight, level, index)
	if width < 1<<logTileHeight {
		path += fmt.Sprintf(".p/%d", width)
	}
	if data, ok := l.tiles[path]; ok {
		return data, nil
	}
	data, err := l.get(path)
	if err != nil && width < 1<<logTileHeight {
		data, err = l.get(fmt.Sprintf("tile/%d/%d/%d", logTileHeight, level, index))
	}
	if err != nil {
		return nil, err
	}
	if len(data)%sha256.Size != 0 {
		return nil, fmt.Errorf("tile %s holds %d bytes, which is not a whole number of hashes", path, len(data))
	}
	l.tiles[path] = data
	return data, nil
}

// nodeHash returns the hash of one node of the log: level 0 holds the record
// hashes, and level L holds the hashes of the pairs of level L-1.
func (l *logClient) nodeHash(level, index int64) ([]byte, error) {
	tileIndex := index >> logTileHeight
	offset := index & (1<<logTileHeight - 1)
	// The last tile of a level holds only as many hashes as the level has.
	width := int64(1) << logTileHeight
	if level < 63 {
		hashes := (l.size + (int64(1) << level) - 1) >> level
		if left := hashes - tileIndex<<logTileHeight; left < width {
			width = left
		}
	}
	data, err := l.tile(level, tileIndex, width)
	if err != nil {
		return nil, err
	}
	start := offset * sha256.Size
	if start+sha256.Size > int64(len(data)) {
		return nil, fmt.Errorf("tile %d/%d/%d holds no hash at %d", logTileHeight, level, tileIndex, offset)
	}
	return data[start : start+sha256.Size], nil
}

// A logStep is one sibling on the way from a record to the root of the log.
type logStep struct {
	Level int64
	Index int64
	// Right marks a sibling that comes after the hash being folded.
	Right bool
}

// auditPath returns the siblings that prove a record is in a log of the given
// size. The tree is the one the log rules build: a range is split at the
// largest power of two smaller than it, so every sibling is a whole subtree
// and its hash is a node of the log.
func auditPath(index, size int64) []logStep {
	var out []logStep
	lo, hi, at := int64(0), size, index
	for hi-lo > 1 {
		k := int64(1) << uint(bits.Len64(uint64(hi-lo-1))-1)
		if at < k {
			out = append(out, nodeOf(lo+k, hi, true))
			hi = lo + k
			continue
		}
		out = append(out, nodeOf(lo, lo+k, false))
		lo, at = lo+k, at-k
	}
	return out
}

// nodeOf returns where a whole subtree's hash is stored.
func nodeOf(lo, hi int64, right bool) logStep {
	level := int64(bits.Len64(uint64(hi-lo)) - 1)
	return logStep{Level: level, Index: lo >> uint(level), Right: right}
}

// root returns the root a record and its siblings fold up to, which has to be
// the hash the signed head names.
func (l *logClient) root(record []byte, index int64) ([]byte, error) {
	hash := tlogLeaf(record)
	for _, step := range auditPath(index, l.size) {
		sibling, err := l.nodeHash(step.Level, step.Index)
		if err != nil {
			return nil, err
		}
		if step.Right {
			hash = tlogNode(hash, sibling)
		} else {
			hash = tlogNode(sibling, hash)
		}
	}
	return hash, nil
}

// A Lookup is what the database holds about one module version.
type Lookup struct {
	// Record is the text the log holds for it, which is the checksum file's
	// lines for that module version.
	Record string
	// Head is the tree the record was proved to be in.
	Head treeHead
}

// Lookup reads what the database holds about a module version and proves that
// it is in the tree the database signed. A record that does not fold up to
// the signed head is refused: that is the whole point of reading a log rather
// than a database.
func (d *SumDB) Lookup(client *http.Client, path, version string) (Lookup, error) {
	if d == nil || d.Key == nil {
		return Lookup{}, fmt.Errorf("checksum database: no key is known")
	}
	l := newLogClient(d, client)
	body, err := l.get("lookup/" + escapePath(path) + "@" + escapePath(version))
	if err != nil {
		return Lookup{}, err
	}
	// The answer is the record number, the record, and a note whose text is
	// the tree the record is in.
	index, record, note, err := splitLookup(body)
	if err != nil {
		return Lookup{}, err
	}
	text, err := VerifyNote(note, d)
	if err != nil {
		return Lookup{}, err
	}
	head, err := parseTreeHead(text)
	if err != nil {
		return Lookup{}, err
	}
	l.size = head.Size
	root, err := l.root([]byte(record), index)
	if err != nil {
		return Lookup{}, err
	}
	if !bytes.Equal(root, head.Hash) {
		return Lookup{}, fmt.Errorf("checksum database: the record for %s %s is not in the tree the database signed", path, version)
	}
	return Lookup{Record: record, Head: head}, nil
}

// splitLookup reads the answer of the lookup endpoint: the record number, the
// record, and the note that follows them.
func splitLookup(body []byte) (index int64, record string, note []byte, err error) {
	first := bytes.IndexByte(body, '\n')
	if first < 0 {
		return 0, "", nil, fmt.Errorf("checksum database: the answer names no record")
	}
	index, err = strconv.ParseInt(strings.TrimSpace(string(body[:first])), 10, 64)
	if err != nil || index < 0 {
		return 0, "", nil, fmt.Errorf("checksum database: the answer names no record number: %q", body[:first])
	}
	rest := body[first+1:]
	split := bytes.Index(rest, []byte("\n\n"))
	if split < 0 {
		return 0, "", nil, fmt.Errorf("checksum database: the answer holds no signed tree")
	}
	record = string(rest[:split])
	if !strings.HasSuffix(record, "\n") {
		record += "\n"
	}
	return index, record, rest[split+2:], nil
}

// tlogLeaf returns the hash of one record of the log. A leaf is told from an
// interior node by the byte in front of it, so no record can be made to look
// like a node.
func tlogLeaf(data []byte) []byte {
	h := sha256.New()
	h.Write([]byte{0})
	h.Write(data)
	return h.Sum(nil)
}

// tlogNode returns the hash of an interior node of the log.
func tlogNode(left, right []byte) []byte {
	h := sha256.New()
	h.Write([]byte{1})
	h.Write(left)
	h.Write(right)
	return h.Sum(nil)
}

// tlogEmpty returns the hash of an empty log.
func tlogEmpty() []byte {
	return sha256.New().Sum(nil)
}

// treeHead is the size and the hash of a log at one moment.
type treeHead struct {
	Size int64
	Hash []byte
}

// parseTreeHead reads the description a database publishes, which is the text
// of its note:
//
//	go.sum database tree
//	12345
//	<base64 hash>
func parseTreeHead(text string) (treeHead, error) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 3 {
		return treeHead{}, fmt.Errorf("the tree description has %d lines, want 3", len(lines))
	}
	size, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil || size < 0 {
		return treeHead{}, fmt.Errorf("the tree description names no size: %q", lines[1])
	}
	hash, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[2]))
	if err != nil || len(hash) != sha256.Size {
		return treeHead{}, fmt.Errorf("the tree description names no hash: %q", lines[2])
	}
	return treeHead{Size: size, Hash: hash}, nil
}

// formatTreeHead writes a tree description the way a database publishes it.
func formatTreeHead(head treeHead) string {
	return fmt.Sprintf("go.sum database tree\n%d\n%s\n",
		head.Size, base64.StdEncoding.EncodeToString(head.Hash))
}
