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
