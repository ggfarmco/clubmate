package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/tunnel"
)

const (
	securityBin     = "/usr/bin/security"
	keychainService = "Clubmate tunnel key"
	keychainLabel   = "Clubmate relay key"
	keyFileName     = "tunnel-key"
	oldSuffix       = ".old"
	securityTimeout = 30 * time.Second
	securityMissing = 44
)

var errNoKey = errors.New("no relay key")

type keyStore interface {
	load(ctx context.Context) ([]byte, error)
	save(ctx context.Context, key []byte) error
	remove(ctx context.Context) error
	old() keyStore
}

type keychainStore struct {
	bin      string
	account  string
	keychain string
}

type fileStore struct {
	path string
}

func newKeyStore(account string) keyStore {
	if os.Getenv(testModeEnv) == "1" {
		return fileStore{path: filepath.Join(pluginsdk.StateDir(), keyFileName)}
	}
	return keychainStore{bin: securityBin, account: account}
}

func loadOrCreateKey(ctx context.Context, ks keyStore) (*tunnel.Static, error) {
	b, err := ks.load(ctx)
	if errors.Is(err, errNoKey) {
		s, err := tunnel.NewStatic()
		if err != nil {
			return nil, err
		}
		if err := ks.save(ctx, s.Bytes()); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	return tunnel.LoadStatic(b)
}

func decodeStoredKey(raw []byte) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(b) != tunnel.KeySize {
		return nil, fmt.Errorf("relay key: stored value is not a %d-byte key", tunnel.KeySize)
	}
	return b, nil
}

func (k keychainStore) load(ctx context.Context) ([]byte, error) {
	out, err := k.run(ctx, "find-generic-password", "-w")
	if err != nil {
		return nil, err
	}
	return decodeStoredKey(out)
}

func (k keychainStore) save(ctx context.Context, key []byte) error {
	_, err := k.run(ctx, "add-generic-password", "-U", "-l", quote(keychainLabel), "-w", quote(base64.StdEncoding.EncodeToString(key)))
	return err
}

func (k keychainStore) remove(ctx context.Context) error {
	_, err := k.run(ctx, "delete-generic-password")
	if errors.Is(err, errNoKey) {
		return nil
	}
	return err
}

func (k keychainStore) old() keyStore {
	k.account += oldSuffix
	return k
}

func (k keychainStore) run(ctx context.Context, verb string, args ...string) ([]byte, error) {
	line := append([]string{verb, "-s", quote(keychainService), "-a", quote(k.account)}, args...)
	if k.keychain != "" {
		line = append(line, quote(k.keychain))
	}
	ctx, cancel := context.WithTimeout(ctx, securityTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, k.bin, "-i")
	cmd.Stdin = strings.NewReader(strings.Join(line, " ") + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == securityMissing {
		return nil, errNoKey
	}
	if err != nil {
		return nil, fmt.Errorf("relay key not readable from the Keychain: %s", securityMessage(stderr.String(), err))
	}
	return stdout.Bytes(), nil
}

func securityMessage(stderr string, err error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if msg := strings.TrimSpace(strings.TrimPrefix(lines[0], "security: ")); msg != "" {
		return msg
	}
	return err.Error()
}

func quote(s string) string {
	return strconv.Quote(s)
}

func (f fileStore) load(context.Context) ([]byte, error) {
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNoKey
	}
	if err != nil {
		return nil, err
	}
	return decodeStoredKey(raw)
}

func (f fileStore) remove(context.Context) error {
	err := os.Remove(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (f fileStore) old() keyStore {
	return fileStore{path: f.path + oldSuffix}
}

func (f fileStore) save(_ context.Context, key []byte) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}
