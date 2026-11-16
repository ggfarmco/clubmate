package rpc_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/pkg/report"
)

func phoneReport(mac bool, names ...string) map[string]any {
	files := []map[string]any{}
	for _, name := range names {
		files = append(files, map[string]any{"name": name, "data": []byte("data of " + name)})
	}
	return map[string]any{"text": "can't connect", "device": "Cali's iPhone", "model": "iPhone17,1", "appVersion": "0.0.71", "mac": mac, "files": files}
}

func (f *reportFixture) submit(t *testing.T, params map[string]any) (map[string]string, error) {
	t.Helper()
	var out map[string]string
	err := f.h.PluginCallResult(t, "remote", "report.submit", params, &out)
	return out, err
}

func (f *reportFixture) uploaded(t *testing.T) (map[string]string, report.Meta) {
	t.Helper()
	f.router.mu.Lock()
	defer f.router.mu.Unlock()
	assert.NoFileExists(t, f.router.zipPath)
	bundle := filepath.Join(t.TempDir(), "report.zip")
	require.NoError(t, os.WriteFile(bundle, f.router.zip, 0o600))
	meta, err := report.ReadMeta(f.router.zip)
	require.NoError(t, err)
	return readZip(t, bundle), meta
}

func TestReportSubmitIsOnlyForRemote(t *testing.T) {
	f := newReportFixture(t)

	err := f.h.Client(t).Call(t.Context(), "report.submit", phoneReport(false, "phone/events.log"), nil)
	assert.ErrorContains(t, err, "report.submit is only for the remote plugin")
	err = f.h.PluginCall(t, "git", "report.submit", phoneReport(false, "phone/events.log"))
	assert.ErrorContains(t, err, "report.submit is only for the remote plugin")
	assert.Empty(t, f.leftovers(t))
}

func TestReportSubmitRefusesBadFileNames(t *testing.T) {
	f := newReportFixture(t)
	many := make([]string, 21)
	for i := range many {
		many[i] = fmt.Sprintf("phone/f%d.log", i)
	}
	for name, params := range map[string]map[string]any{
		"dotdot":    phoneReport(false, "../x"),
		"escape":    phoneReport(false, "phone/../../x"),
		"absolute":  phoneReport(false, "/abs"),
		"hidden":    phoneReport(false, "phone/.git"),
		"space":     phoneReport(false, "phone/A B"),
		"elsewhere": phoneReport(false, "notphone/x"),
		"twice":     phoneReport(false, "phone/a.log", "phone/a.log"),
		"too many":  phoneReport(false, many...),
		"no text":   {"text": "  ", "files": []any{}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.submit(t, params)
			assert.ErrorIs(t, err, rpc.ErrInvalidParams)
			assert.Empty(t, f.leftovers(t))
		})
	}
	f.router.mu.Lock()
	defer f.router.mu.Unlock()
	assert.Empty(t, f.router.zipPath)
}

func TestReportSubmitBundlesMacLogsAndPhoneFiles(t *testing.T) {
	f := newReportFixture(t)
	require.NoError(t, os.WriteFile(f.h.Dirs.CoreLog(), []byte("core says hi\n"), 0o600))
	require.NoError(t, os.MkdirAll(f.h.Dirs.PluginsDir(), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(f.h.Dirs.PluginsDir(), "remote.log"), []byte("remote: auth err=unknown\n"), 0o600))

	res, err := f.submit(t, phoneReport(true, "phone/events.log", "phone/info.json"))

	require.NoError(t, err)
	assert.Equal(t, map[string]string{"id": "rpt_1"}, res)
	files, meta := f.uploaded(t)
	for _, name := range []string{"info.json", "logs/core.log", "logs/plugins/remote.log", "plugins.json", "report.json"} {
		assert.Contains(t, files, name)
	}
	assert.Equal(t, "data of phone/events.log", files["phone/events.log"])
	assert.Equal(t, "data of phone/info.json", files["phone/info.json"])
	for name := range files {
		assert.False(t, name == "sessions.json" || name == "workspaces.json" || name == "settings.json" || strings.HasPrefix(name, "ui/"), name)
	}
	assert.Equal(t, [5]string{"phone", "Cali's iPhone", "iPhone17,1", "0.0.71", "can't connect"}, [5]string{meta.Source, meta.Device, meta.Model, meta.AppVersion, meta.Text})
	assert.Contains(t, meta.Files, "logs/core.log")
	assert.Empty(t, f.leftovers(t))
}

func TestReportSubmitWithoutMacPartHasOnlyPhoneFiles(t *testing.T) {
	f := newReportFixture(t)
	require.NoError(t, os.WriteFile(f.h.Dirs.CoreLog(), []byte("core says hi\n"), 0o600))

	_, err := f.submit(t, phoneReport(false, "phone/events.log"))

	require.NoError(t, err)
	files, meta := f.uploaded(t)
	assert.Equal(t, []string{"README.txt", "phone/events.log", "report.json"}, slices.Sorted(maps.Keys(files)))
	assert.Equal(t, []string{"phone/events.log"}, meta.Files)
}
