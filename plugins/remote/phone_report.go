package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

const reportSubmitMethod = "report.submit"

type phoneReportRequest struct {
	RequestID  string      `json:"requestId"`
	Text       string      `json:"text"`
	Device     string      `json:"device"`
	Model      string      `json:"model"`
	AppVersion string      `json:"appVersion"`
	Mac        bool        `json:"mac"`
	Files      []phoneFile `json:"files"`
}

type phoneFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

type submitReport struct {
	Text       string      `json:"text"`
	Device     string      `json:"device"`
	Model      string      `json:"model"`
	AppVersion string      `json:"appVersion"`
	Mac        bool        `json:"mac"`
	Files      []phoneFile `json:"files"`
}

type phoneReportResult struct {
	ID string `json:"id"`
}

func finalReportError(message string) bool {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestEntityTooLarge} {
		if strings.Contains(message, reportRefusals[status].Error()) {
			return true
		}
	}
	return strings.Contains(message, errReportTooBig.Error())
}

func (s *server) report(ctx context.Context, d device, p phoneReportRequest) (json.RawMessage, error) {
	if !validRequestID(p.RequestID) {
		return nil, &apiError{Code: codeInvalidParams, Message: "requestId is required: at most 64 letters, digits, '-' or '_'"}
	}
	return s.requests.do(ctx, p.RequestID, func(ctx context.Context) (json.RawMessage, error) {
		size := 0
		for _, f := range p.Files {
			size += len(f.Data)
		}
		id, err := s.backend.SubmitReport(ctx, submitReport{Text: p.Text, Device: d.Name, Model: d.Model, AppVersion: p.AppVersion, Mac: p.Mac, Files: p.Files})
		if err != nil {
			api := toAPIError(err)
			logEvent("report", "device", logID(d.ID), "bytes", strconv.Itoa(size), "err", api.Code)
			if outcomeUnknown(err) || api.Code == codeInvalidParams || finalReportError(api.Message) {
				return nil, err
			}
			return nil, &apiError{Code: codeFailed, Message: api.Message}
		}
		logEvent("report", "device", logID(d.ID), "bytes", strconv.Itoa(size), "id", id)
		return json.Marshal(phoneReportResult{ID: id})
	})
}
