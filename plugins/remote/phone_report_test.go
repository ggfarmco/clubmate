package main

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func phoneReportParams(requestID string) map[string]any {
	return map[string]any{"requestId": requestID, "text": "can't connect", "device": "Someone else", "model": "Fake", "appVersion": "0.0.71", "mac": true, "files": []map[string]any{{"name": "phone/events.log", "data": []byte("12:00 offline")}}}
}

func (b *fakeBackend) SubmitReport(_ context.Context, p submitReport) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reports = append(b.reports, p)
	return "rpt_" + strconv.Itoa(len(b.reports)), nil
}

func (b *fakeBackend) submitted() []submitReport {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]submitReport(nil), b.reports...)
}

func TestReportIsDedupedByRequestID(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")

	_, first := f.call(t, token, "report", phoneReportParams("r1"))
	_, second := f.call(t, token, "report", phoneReportParams("r1"))

	require.Nil(t, first.Error)
	assert.JSONEq(t, `{"id":"rpt_1"}`, string(first.Result))
	assert.JSONEq(t, string(first.Result), string(second.Result))
	assert.Len(t, f.backend.submitted(), 1)
}

func TestReportNeedsARequestID(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")

	for _, id := range []string{"", "bad id"} {
		_, out := f.call(t, token, "report", phoneReportParams(id))
		require.NotNil(t, out.Error, id)
		assert.Equal(t, codeInvalidParams, out.Error.Code)
	}
	assert.Empty(t, f.backend.submitted())
}

func TestReportPassesTheDeviceName(t *testing.T) {
	f := newFixture(t)
	token := f.pairDevice(t, deviceInfo{ID: "phone", Name: "Cali's iPhone", Model: "iPhone17,1"}).Token

	_, out := f.call(t, token, "report", phoneReportParams("r1"))

	require.Nil(t, out.Error)
	assert.Equal(t, []submitReport{{Text: "can't connect", Device: "Cali's iPhone", Model: "iPhone17,1", AppVersion: "0.0.71", Mac: true, Files: []phoneFile{{Name: "phone/events.log", Data: []byte("12:00 offline")}}}}, f.backend.submitted())
}
