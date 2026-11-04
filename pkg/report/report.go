package report

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	HeaderKey       = "X-Clubmate-Key"
	HeaderTime      = "X-Clubmate-Time"
	HeaderSignature = "X-Clubmate-Signature"

	MetaName    = "report.json"
	StoredName  = "stored.json"
	HandledName = ".handled"
	MaxBundle   = 25 << 20
	MaxEntry    = 25 << 20
	MaxUnpacked = 100 << 20
	MaxEntries  = 10000
	MaxName     = 1000
	MaxNamePart = 255
	MaxText     = 20 << 10
	MaxSkew     = 5 * time.Minute

	maxMetaJSON = 1 << 20
)

var (
	ErrUnsigned     = errors.New("request is not signed")
	ErrSkew         = errors.New("signature time is too far off")
	ErrBadSignature = errors.New("bad signature")
	ErrBadMeta      = errors.New("bad report.json")
	ErrBadBundle    = errors.New("unsafe report bundle")
)

type Meta struct {
	Text         string   `json:"text"`
	Summary      string   `json:"summary"`
	SessionID    string   `json:"sessionId"`
	SessionTitle string   `json:"sessionTitle"`
	AppVersion   string   `json:"appVersion"`
	CoreVersion  string   `json:"coreVersion"`
	Commit       string   `json:"commit"`
	MacOS        string   `json:"macOS"`
	Arch         string   `json:"arch"`
	Files        []string `json:"files"`
}

type Stored struct {
	ID       string    `json:"id"`
	Received time.Time `json:"received"`
	Trusted  bool      `json:"trusted"`
	Mac      string    `json:"mac,omitempty"`
	Key      string    `json:"key"`
	IP       string    `json:"ip"`
	Bytes    int64     `json:"bytes"`
	Report   Meta      `json:"report"`
}

func Sign(req *http.Request, priv ed25519.PrivateKey, body []byte, now time.Time) {
	ts := strconv.FormatInt(now.Unix(), 10)
	pub, _ := priv.Public().(ed25519.PublicKey)
	sig := ed25519.Sign(priv, message(req, ts, body))
	req.Header.Set(HeaderKey, base64.RawURLEncoding.EncodeToString(pub))
	req.Header.Set(HeaderTime, ts)
	req.Header.Set(HeaderSignature, base64.RawURLEncoding.EncodeToString(sig))
}

func Verify(req *http.Request, body []byte, now time.Time) (ed25519.PublicKey, error) {
	rawKey, ts, rawSig := req.Header.Get(HeaderKey), req.Header.Get(HeaderTime), req.Header.Get(HeaderSignature)
	if rawKey == "" || ts == "" || rawSig == "" {
		return nil, ErrUnsigned
	}
	key, err := base64.RawURLEncoding.Strict().DecodeString(rawKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, ErrBadSignature
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, ErrBadSignature
	}
	if d := now.Sub(time.Unix(sec, 0)); d > MaxSkew || d < -MaxSkew {
		return nil, ErrSkew
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(rawSig)
	if err != nil || !ed25519.Verify(key, message(req, ts, body), sig) {
		return nil, ErrBadSignature
	}
	return ed25519.PublicKey(key), nil
}

func KeyID(pub ed25519.PublicKey) string {
	id := hex.EncodeToString(pub)
	return id[:min(len(id), 8)]
}

func ReadMeta(bundle []byte) (Meta, error) {
	if len(bundle) > MaxBundle {
		return Meta{}, fmt.Errorf("%w: bundle over %d bytes", ErrBadMeta, MaxBundle)
	}
	zr, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %v", ErrBadMeta, err)
	}
	if err := CheckEntries(zr); err != nil {
		return Meta{}, err
	}
	f, err := zr.Open(MetaName)
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %v", ErrBadMeta, err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxMetaJSON+1))
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %v", ErrBadMeta, err)
	}
	if len(raw) > maxMetaJSON {
		return Meta{}, fmt.Errorf("%w: over %d bytes", ErrBadMeta, maxMetaJSON)
	}
	var m Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return Meta{}, fmt.Errorf("%w: %v", ErrBadMeta, err)
	}
	switch {
	case strings.TrimSpace(m.Text) == "":
		return Meta{}, fmt.Errorf("%w: text is empty", ErrBadMeta)
	case len(m.Text) > MaxText:
		return Meta{}, fmt.Errorf("%w: text over %d bytes", ErrBadMeta, MaxText)
	case len(m.Summary) > MaxText:
		return Meta{}, fmt.Errorf("%w: summary over %d bytes", ErrBadMeta, MaxText)
	}
	return m, nil
}

func CheckEntries(zr *zip.Reader) error {
	if len(zr.File) > MaxEntries {
		return fmt.Errorf("%w: over %d entries", ErrBadBundle, MaxEntries)
	}
	var total uint64
	for _, f := range zr.File {
		name := filepath.FromSlash(f.Name)
		switch {
		case !creatable(f.Name):
			return fmt.Errorf("%w: entry %q has a name no file system takes", ErrBadBundle, f.Name)
		case !filepath.IsLocal(name) || reserved(filepath.Clean(name)):
			return fmt.Errorf("%w: entry %q", ErrBadBundle, f.Name)
		case f.Mode()&fs.ModeType&^fs.ModeDir != 0:
			return fmt.Errorf("%w: entry %q is not a regular file", ErrBadBundle, f.Name)
		case f.UncompressedSize64 > MaxEntry:
			return fmt.Errorf("%w: entry %q is over %d MiB", ErrBadBundle, f.Name, MaxEntry>>20)
		}
		total += f.UncompressedSize64
		if total > MaxUnpacked {
			return fmt.Errorf("%w: over %d MiB unpacked", ErrBadBundle, MaxUnpacked>>20)
		}
	}
	return nil
}

func NewID(now time.Time) string {
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return "rpt_" + now.UTC().Format("20060102T150405Z") + "_" + hex.EncodeToString(suffix)
}

func message(req *http.Request, ts string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(req.Method + "\n" + req.URL.Path + "\n" + ts + "\n" + hex.EncodeToString(sum[:]))
}

func creatable(name string) bool {
	if len(name) > MaxName || strings.ContainsRune(name, 0) || !utf8.ValidString(name) {
		return false
	}
	for part := range strings.SplitSeq(name, "/") {
		if len(part) > MaxNamePart {
			return false
		}
	}
	return true
}

func reserved(name string) bool {
	first, _, _ := strings.Cut(name, string(filepath.Separator))
	return strings.EqualFold(first, StoredName) || strings.EqualFold(first, HandledName)
}
