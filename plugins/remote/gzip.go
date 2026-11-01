package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

var gzipWriters = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

type gzipResponse struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func gzipped(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next(w, r)
			return
		}
		gz := gzipWriters.Get().(*gzip.Writer)
		gz.Reset(w)
		defer func() {
			_ = gz.Close()
			gzipWriters.Put(gz)
		}()
		w.Header().Set("Content-Encoding", "gzip")
		next(gzipResponse{ResponseWriter: w, gz: gz}, r)
	}
}

func (g gzipResponse) Write(p []byte) (int, error) {
	return g.gz.Write(p)
}

func (g gzipResponse) Unwrap() http.ResponseWriter {
	return g.ResponseWriter
}

func acceptsGzip(header string) bool {
	for part := range strings.SplitSeq(header, ",") {
		coding, params, _ := strings.Cut(part, ";")
		if strings.EqualFold(strings.TrimSpace(coding), "gzip") && strings.ReplaceAll(strings.TrimSpace(params), " ", "") != "q=0" {
			return true
		}
	}
	return false
}
