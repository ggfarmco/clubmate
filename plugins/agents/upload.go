package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	uploadsDir   = "uploads"
	uploadedName = "file"
)

var errBadUpload = errors.New("attached file data is not base64")

func (a *app) saveUploads(session string, atts []attachment) error {
	for i := range atts {
		att := &atts[i]
		if att.Kind != "file" || att.Data == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(att.Data)
		if err != nil {
			return fmt.Errorf("%w: %s", errBadUpload, att.Name)
		}
		name := filepath.Base(att.Name)
		if name == "." || name == ".." || name == string(filepath.Separator) {
			name = uploadedName
		}
		dir := filepath.Join(a.stateDir, uploadsDir, session, rand.Text())
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		att.Path, att.Data = path, ""
	}
	return nil
}

func (a *app) dropUploads(session string) error {
	return os.RemoveAll(filepath.Join(a.stateDir, uploadsDir, session))
}
