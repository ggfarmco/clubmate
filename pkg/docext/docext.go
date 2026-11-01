package docext

import (
	"path/filepath"
	"strings"
)

func Is(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mmd":
		return true
	}
	return false
}

func IsArtifact(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return true
	}
	return false
}
