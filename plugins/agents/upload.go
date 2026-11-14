package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

const (
	uploadsDir   = "uploads"
	uploadedName = "file"
	imageName    = "image"

	queuedImageBytes = 10 << 20
)

var errBadUpload = errors.New("attached file data is not base64")

func (a *app) saveUploads(session string, atts []attachment) error {
	for i := range atts {
		att := &atts[i]
		if att.Kind != "file" && att.Kind != "image" || att.Data == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(att.Data)
		if err != nil {
			return fmt.Errorf("%w: %s", errBadUpload, att.Name)
		}
		name := filepath.Base(att.Name)
		switch {
		case att.Kind == "image":
			name = imageName
		case name == "." || name == ".." || name == string(filepath.Separator):
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
		att.Path = path
		if att.Kind == "file" {
			att.Data = ""
		}
	}
	return nil
}

func (a *app) withImages(session string, args promptArgs) promptArgs {
	args.Attachments = slices.Clone(args.Attachments)
	for i, att := range args.Attachments {
		if att.Kind == "image" && att.Data == "" && att.Path != "" {
			args.Attachments[i].Data = a.uploadedImage(session, att.Path)
		}
	}
	return args
}

func (a *app) uploadedImage(session, path string) string {
	base, err := filepath.EvalSymlinks(filepath.Join(a.stateDir, uploadsDir, session))
	if err != nil {
		return ""
	}
	if path, err = filepath.EvalSymlinks(path); err != nil {
		return ""
	}
	if rel, err := filepath.Rel(base, path); err != nil || !filepath.IsLocal(rel) {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, queuedImageBytes+1))
	if err != nil || len(data) > queuedImageBytes {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func (a *app) dropUploads(session string) error {
	return os.RemoveAll(filepath.Join(a.stateDir, uploadsDir, session))
}
