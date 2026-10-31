package jsonrpc2

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestReadValueFramesConcatenatedMessages(t *testing.T) {
	input := " {\"a\":\"}\\\"{\"}\n[1,{\"b\":[]}]{\"c\":1}"
	r := bufio.NewReaderSize(iotest.OneByteReader(strings.NewReader(input)), 16)
	var got []string
	for {
		raw, err := readValue(r, nil)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(raw))
	}
	want := []string{`{"a":"}\"{"}`, `[1,{"b":[]}]`, `{"c":1}`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadValueRejectsTruncatedAndScalar(t *testing.T) {
	if _, err := readValue(bufio.NewReader(strings.NewReader(`{"a":`)), nil); err != io.ErrUnexpectedEOF {
		t.Fatalf("truncated: got %v", err)
	}
	if _, err := readValue(bufio.NewReader(strings.NewReader(`42`)), nil); err == nil {
		t.Fatal("scalar: want error")
	}
}

func TestWriteObjectKeepsOneMessagePerLine(t *testing.T) {
	var out bytes.Buffer
	s := NewPlainObjectStream(nopCloser{&out})
	params := json.RawMessage("{\n  \"tree\": [1,\n 2]\n}")
	req := &Request{Method: "ui.updated", Params: &params, Notif: true}
	if err := s.WriteObject(&anyMessage{request: req}); err != nil {
		t.Fatal(err)
	}
	want := `{"jsonrpc":"2.0","method":"ui.updated","params":{"tree":[1,2]}}` + "\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestSetParamsPassesRawMessageThrough(t *testing.T) {
	raw := json.RawMessage(`{"tree":"<b>"}`)
	var req Request
	if err := req.SetParams(raw); err != nil {
		t.Fatal(err)
	}
	if string(*req.Params) != string(raw) {
		t.Fatalf("got %s", *req.Params)
	}
}

type nopCloser struct{ io.ReadWriter }

func (nopCloser) Close() error { return nil }
