package distro_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/neovim/go-client/nvim"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

type opts struct {
	UserLayer      map[string]string
	ThemeFile      string
	ReadOnlyDistro bool
	LongPath       bool
	StateDir       string
}

var errLine = regexp.MustCompile(`E\d+:|Error|stack traceback`)

const notifierErrors = `
local notifier = package.loaded["snacks.notifier"]
local out = {}
if not notifier then return out end
for _, n in ipairs(notifier.get_history()) do
  if n.level == "error" or n.level == vim.log.levels.ERROR then
    out[#out + 1] = tostring(n.msg)
  end
end
return out`

const netShim = `#!/bin/sh
printf '%s\n' "@NAME@ $*" >> '@HOME@/net.log'
exit 1
`

const gitShim = `#!/bin/sh
for arg in "$@"; do
  case "$skip$arg" in
    1*) skip= ;;
    -C|-c|--git-dir|--work-tree|--namespace|--exec-path|--super-prefix) skip=1 ;;
    -*) ;;
    clone|fetch|ls-remote|pull|push) printf '%s\n' "git $*" >> '@HOME@/net.log'; exit 1 ;;
    *) exec /usr/bin/git "$@" ;;
  esac
done
exec /usr/bin/git "$@"
`

func resources(t *testing.T) string {
	t.Helper()
	res := os.Getenv("CLUBMATE_TEST_RESOURCES")
	if res == "" {
		t.Skip("CLUBMATE_TEST_RESOURCES is not set; run make nvim-test")
	}
	return res
}

func startDistro(t *testing.T, o opts) *nvim.Nvim {
	t.Helper()
	res := resources(t)
	home := t.TempDir()
	distro := filepath.Join(res, "nvim-distro")
	if o.LongPath {
		distro = copyDistro(t, distro, longDir(t))
	}
	if o.ReadOnlyDistro {
		distro = readOnlyCopy(t, distro)
	}
	shims := filepath.Join(home, "shims")
	require.NoError(t, os.Mkdir(shims, 0o755))
	for name, script := range map[string]string{"git": gitShim, "curl": netShim, "wget": netShim} {
		script = strings.NewReplacer("@NAME@", name, "@HOME@", home).Replace(script)
		require.NoError(t, os.WriteFile(filepath.Join(shims, name), []byte(script), 0o755))
	}
	env := []string{
		"HOME=" + home,
		"NVIM_APPNAME=clubmate-test",
		"PATH=" + strings.Join([]string{shims, filepath.Join(res, "tools"), "/usr/bin", "/bin"}, ":"),
		"TMPDIR=" + os.TempDir(),
	}
	for k, rel := range map[string]string{"XDG_CONFIG_HOME": ".config", "XDG_DATA_HOME": ".local/share", "XDG_STATE_HOME": ".local/state", "XDG_CACHE_HOME": ".cache"} {
		dir := filepath.Join(home, rel)
		if k == "XDG_STATE_HOME" && o.StateDir != "" {
			dir = o.StateDir
		}
		require.NoError(t, os.MkdirAll(dir, 0o755))
		env = append(env, k+"="+dir)
	}
	if o.ThemeFile != "" {
		env = append(env, "CLUBMATE_THEME_FILE="+o.ThemeFile)
	}
	for rel, content := range o.UserLayer {
		p := filepath.Join(home, ".config", "clubmate-test", rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	v, err := nvim.NewChildProcess(
		nvim.ChildProcessCommand(filepath.Join(res, "nvim", "bin", "nvim")),
		nvim.ChildProcessArgs("--embed", "-u", filepath.Join(distro, "init.lua")),
		nvim.ChildProcessEnv(env),
		nvim.ChildProcessDir(home),
		nvim.ChildProcessLogf(func(string, ...any) {}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close() })
	require.NoError(t, v.RegisterHandler("redraw", func(...[]any) {}))
	require.NoError(t, v.AttachUI(120, 40, map[string]any{"ext_linegrid": true}))
	testutil.Eventually(t, func() bool {
		var ready int
		require.NoError(t, v.Eval("v:vim_did_enter && get(g:, 'clubmate_ready', v:false)", &ready))
		return ready == 1
	}, 15*time.Second)
	return v
}

func copyDistro(t *testing.T, src, parent string) string {
	t.Helper()
	dst := filepath.Join(parent, "nvim-distro")
	out, err := exec.Command("cp", "-Rc", src, dst).CombinedOutput()
	require.NoError(t, err, string(out))
	return dst
}

func longDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; len(dir) < 200; i++ {
		dir = filepath.Join(dir, fmt.Sprintf("Build-Products-Debug-%02d-abcdefghijklmnop", i))
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

func readOnlyCopy(t *testing.T, src string) string {
	t.Helper()
	dst := copyDistro(t, src, t.TempDir())
	t.Cleanup(func() { _ = exec.Command("chmod", "-R", "u+w", dst).Run() })
	out, err := exec.Command("chmod", "-R", "a-w", dst).CombinedOutput()
	require.NoError(t, err, string(out))
	return dst
}

func errorsOf(t *testing.T, v *nvim.Nvim) []string {
	t.Helper()
	var errs []string
	var errmsg string
	require.NoError(t, v.Eval("v:errmsg", &errmsg))
	if errmsg != "" {
		errs = append(errs, errmsg)
	}
	msgs, err := v.Exec("messages", true)
	require.NoError(t, err)
	for line := range strings.SplitSeq(msgs, "\n") {
		if errLine.MatchString(line) {
			errs = append(errs, line)
		}
	}
	var notes []string
	require.NoError(t, v.ExecLua(notifierErrors, &notes))
	return append(errs, notes...)
}

func lockedPlugins(t *testing.T) map[string]map[string]string {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "scripts", "nvim-distro.lock"))
	require.NoError(t, err)
	defer f.Close()
	plugins := map[string]map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) == 0 || fields[0] != "plugin" {
			continue
		}
		require.Len(t, fields, 5, s.Text())
		plugins[fields[1]] = map[string]string{"url": fields[4], "commit": fields[2]}
	}
	require.NoError(t, s.Err())
	return plugins
}

func TestVendoredPluginsComplete(t *testing.T) {
	v := startDistro(t, opts{ReadOnlyDistro: true})
	var manifest map[string]map[string]string
	require.NoError(t, v.ExecLua(`
local init = vim.v.argv[vim.fn.index(vim.v.argv, "-u") + 2]
return dofile(vim.fs.joinpath(vim.fs.dirname(init), "lua/clubmate/manifest.lua")).plugins`, &manifest))
	want := lockedPlugins(t)
	require.NotEmpty(t, want)
	assert.Equal(t, want, manifest)
	for name := range want {
		dir := filepath.Join(resources(t), "nvim-distro", "plugins", name)
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.NotEmpty(t, entries, name)
		txt, err := filepath.Glob(filepath.Join(dir, "doc", "*.txt"))
		require.NoError(t, err)
		if len(txt) > 0 {
			assert.FileExists(t, filepath.Join(dir, "doc", "tags"))
		}
	}
	assert.Empty(t, errorsOf(t, v))
}

const settledBlink = `
local fuzzy = package.loaded["blink.cmp.fuzzy"]
return fuzzy ~= nil and package.loaded["blink.cmp.completion"] ~= nil`

func settle(t *testing.T, v *nvim.Nvim) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		var done bool
		require.NoError(t, v.ExecLua(settledBlink, &done))
		return done
	}, 10*time.Second)
}

func distroRoot(t *testing.T, v *nvim.Nvim) string {
	t.Helper()
	var root string
	require.NoError(t, v.ExecLua(`return vim.fs.dirname(vim.v.argv[vim.fn.index(vim.v.argv, "-u") + 2])`, &root))
	return root
}

func netLog(t *testing.T, v *nvim.Nvim) string {
	t.Helper()
	var home string
	require.NoError(t, v.Eval("$HOME", &home))
	b, err := os.ReadFile(filepath.Join(home, "net.log"))
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(b)
}

func treeHash(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		fmt.Fprintf(h, "%s\x00", rel)
		if d.Type().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h.Write(b)
		}
		return nil
	}))
	return hex.EncodeToString(h.Sum(nil))
}

func TestStartsClean(t *testing.T) {
	v := startDistro(t, opts{})
	settle(t, v)
	var bad []string
	require.NoError(t, v.ExecLua(`
local root = ...
local plugins = require("lazy.core.config").plugins
local bad = {}
for name in pairs(dofile(root .. "/lua/clubmate/manifest.lua").plugins) do
  local p = plugins[name]
  if not p or not p._.installed or p.dir ~= root .. "/plugins/" .. name then
    bad[#bad + 1] = name
  end
end
return bad`, &bad, distroRoot(t, v)))
	assert.Empty(t, bad)
	var leader string
	require.NoError(t, v.Eval("g:mapleader", &leader))
	assert.Equal(t, ",", leader)
	msgs, err := v.Exec("messages", true)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(msgs))
	assert.Empty(t, errorsOf(t, v))
}

func TestUserLayerOverrides(t *testing.T) {
	v := startDistro(t, opts{UserLayer: map[string]string{
		"lua/user/plugins/x.lua": `return { "folke/which-key.nvim", opts = { delay = 42 } }`,
		"lua/user/options.lua":   `vim.o.tabstop = 2`,
	}})
	var delay, tabstop int
	require.NoError(t, v.ExecLua(`return require("which-key.config").options.delay`, &delay))
	require.NoError(t, v.Eval("&tabstop", &tabstop))
	assert.Equal(t, 42, delay)
	assert.Equal(t, 2, tabstop)
	assert.Empty(t, errorsOf(t, v))
}

func TestUserLayerErrorIsolated(t *testing.T) {
	v := startDistro(t, opts{UserLayer: map[string]string{"lua/user/keymaps.lua": `error("boom")`}})
	errs := errorsOf(t, v)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "boom")
	var rhs map[string]any
	require.NoError(t, v.Call("maparg", &rhs, ",ff", "n", false, true))
	assert.NotEmpty(t, rhs)
}

func TestLegacyConfigCopyIgnored(t *testing.T) {
	v := startDistro(t, opts{UserLayer: map[string]string{
		"init.lua":            `error("legacy")`,
		"lua/plugins/fzf.lua": `return { "ibhagwan/fzf-lua" }`,
	}})
	settle(t, v)
	var fzf bool
	require.NoError(t, v.ExecLua(`return require("lazy.core.config").plugins["fzf-lua"] ~= nil`, &fzf))
	assert.False(t, fzf)
	assert.Empty(t, netLog(t, v))
	assert.Empty(t, errorsOf(t, v))
}

func TestOfflineReadOnly(t *testing.T) {
	v := startDistro(t, opts{ReadOnlyDistro: true})
	settle(t, v)
	require.NoError(t, v.Command("enew | startinsert"))
	waitMode(t, v, "i")
	_, err := v.Input("<Esc>")
	require.NoError(t, err)
	assert.Equal(t, treeHash(t, filepath.Join(resources(t), "nvim-distro")), treeHash(t, distroRoot(t, v)))
	assert.Empty(t, netLog(t, v))
	assert.Empty(t, errorsOf(t, v))
}

const lazyTaskErrors = `
local out = {}
for name, p in pairs(require("lazy.core.config").plugins) do
  for _, task in ipairs(p._.tasks or {}) do
    if task:has_errors() then
      out[#out + 1] = name .. ": " .. task:output(vim.log.levels.ERROR)
    end
  end
end
return out`

func TestLazyUpdateLeavesDistroAlone(t *testing.T) {
	v := startDistro(t, opts{ReadOnlyDistro: true})
	settle(t, v)
	var errs []string
	require.NoError(t, v.ExecLua(`require("lazy").update({ wait = true, show = false })`+lazyTaskErrors, &errs))
	assert.Empty(t, errs)
	assert.Empty(t, netLog(t, v))
	assert.Empty(t, errorsOf(t, v))
}

func TestLongInstallPath(t *testing.T) {
	v := startDistro(t, opts{LongPath: true})
	settle(t, v)
	root := distroRoot(t, v)
	require.GreaterOrEqual(t, len(root), 200)
	assert.Empty(t, errorsOf(t, v))
}

const parserCheck = `
local init = vim.v.argv[vim.fn.index(vim.v.argv, "-u") + 2]
local root = vim.fs.dirname(init)
local site = vim.fs.joinpath(root, "site")
if not vim.tbl_contains(vim.opt.rtp:get(), site) then
  vim.opt.rtp:prepend(site)
end
local out = { failed = {} }
for _, lang in ipairs(dofile(vim.fs.joinpath(root, "lua/clubmate/manifest.lua")).parsers) do
  local ok, added = pcall(vim.treesitter.language.add, lang)
  local qok, query = pcall(vim.treesitter.query.get, lang, "highlights")
  local pok = ok and added and pcall(function() vim.treesitter.get_string_parser("", lang):parse() end)
  if not (ok and added and qok and query and pok) then
    out.failed[#out.failed + 1] = lang
  end
end
out.lua = vim.api.nvim_get_runtime_file("parser/lua.so", false)[1] or ""
out.go = vim.api.nvim_get_runtime_file("parser/go.so", false)[1] or ""
out.site = site
return out`

func TestParsersLoadAndHighlight(t *testing.T) {
	v := startDistro(t, opts{ReadOnlyDistro: true})
	var got struct {
		Failed []string `msgpack:"failed"`
		Lua    string   `msgpack:"lua"`
		Go     string   `msgpack:"go"`
		Site   string   `msgpack:"site"`
	}
	require.NoError(t, v.ExecLua(parserCheck, &got))
	assert.Empty(t, got.Failed)
	assert.True(t, strings.HasPrefix(got.Lua, got.Site+"/"), got.Lua)
	assert.True(t, strings.HasPrefix(got.Go, got.Site+"/"), got.Go)
	assert.Empty(t, errorsOf(t, v))
}

func TestParserLockMatchesTreesitter(t *testing.T) {
	res := resources(t)
	cmd := exec.Command(filepath.Join(res, "nvim", "bin", "nvim"), "--clean", "--headless", "-i", "NONE", "-l",
		filepath.Join("..", "scripts", "nvim-distro", "parsers.lua"), "check",
		filepath.Join(res, "nvim-distro", "plugins", "nvim-treesitter"), filepath.Join("..", "scripts", "nvim-distro.lock"))
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

const blinkSetup = `
local init = vim.v.argv[vim.fn.index(vim.v.argv, "-u") + 2]
local blink = vim.fs.joinpath(vim.fs.dirname(init), "plugins", "blink.cmp")
if not package.loaded["blink.cmp"] then
  vim.opt.rtp:prepend(blink)
  require("blink.cmp").setup({})
end
vim.cmd.enew()
vim.cmd.startinsert()`

func TestBlinkUsesRust(t *testing.T) {
	v := startDistro(t, opts{ReadOnlyDistro: true})
	require.NoError(t, v.ExecLua(blinkSetup, nil))
	var impl string
	blocked := func() bool {
		m, err := v.Mode()
		require.NoError(t, err)
		return m.Blocking
	}
	testutil.Eventually(t, func() bool {
		if blocked() {
			return true
		}
		require.NoError(t, v.ExecLua(`return require("blink.cmp.fuzzy").implementation_type`, &impl))
		return impl == "rust"
	}, 10*time.Second)
	if blocked() {
		for i := 0; i < 20 && blocked(); i++ {
			_, err := v.Input("<CR>")
			require.NoError(t, err)
			time.Sleep(50 * time.Millisecond)
		}
		require.Fail(t, "nvim blocked on a prompt", "%q", errorsOf(t, v))
	}
	var loaded bool
	require.NoError(t, v.ExecLua(`return package.loaded["blink_cmp_fuzzy"] ~= nil`, &loaded))
	assert.True(t, loaded)
	assert.Empty(t, errorsOf(t, v))
}

func TestNrevOnPath(t *testing.T) {
	v := startDistro(t, opts{})
	var path string
	require.NoError(t, v.Eval(`exepath("nrev")`, &path))
	assert.Equal(t, filepath.Join(resources(t), "tools", "nrev"), path)
	out, err := exec.Command(path, "list", "--json", "--db", filepath.Join(t.TempDir(), "review.db")).CombinedOutput()
	require.NoError(t, err, string(out))
}

const notifierWarnings = `
local notifier = package.loaded["snacks.notifier"]
local out = {}
if not notifier then return out end
for _, n in ipairs(notifier.get_history()) do
  if n.level == "warn" or n.level == vim.log.levels.WARN then
    out[#out + 1] = tostring(n.msg)
  end
end
return out`

func serverNotices(t *testing.T, v *nvim.Nvim, cmd string) []string {
	t.Helper()
	require.NoError(t, v.Command(cmd))
	require.NoError(t, v.ExecLua(`vim.notify("barrier", vim.log.levels.WARN)`, nil))
	var warns []string
	testutil.Eventually(t, func() bool {
		require.NoError(t, v.ExecLua(notifierWarnings, &warns))
		return len(warns) > 0 && warns[len(warns)-1] == "barrier"
	}, 5*time.Second)
	var notices []string
	for _, w := range warns {
		if strings.Contains(w, "MasonInstall") {
			notices = append(notices, w)
		}
	}
	return notices
}

func TestMissingServerNoticeOnce(t *testing.T) {
	v := startDistro(t, opts{})
	settle(t, v)
	warns := serverNotices(t, v, "edit x.go")
	require.Len(t, warns, 1)
	assert.Contains(t, warns[0], ":MasonInstall gopls")
	assert.Empty(t, errorsOf(t, v))
	var state string
	require.NoError(t, v.Eval("$XDG_STATE_HOME", &state))
	_ = v.Close()

	v = startDistro(t, opts{StateDir: state})
	settle(t, v)
	assert.Empty(t, serverNotices(t, v, "edit y.go"))
}

func TestServerOnPathNoNotice(t *testing.T) {
	v := startDistro(t, opts{})
	settle(t, v)
	var home string
	require.NoError(t, v.Eval("$HOME", &home))
	require.NoError(t, os.WriteFile(filepath.Join(home, "shims", "gopls"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	assert.Empty(t, serverNotices(t, v, "edit x.go"))
}

func TestCheckhealthClean(t *testing.T) {
	v := startDistro(t, opts{})
	settle(t, v)
	require.NoError(t, v.Command("checkhealth clubmate"))
	var lines []string
	require.NoError(t, v.Call("getline", &lines, 1, "$"))
	report := strings.Join(lines, "\n")
	assert.NotContains(t, report, "ERROR", report)
	assert.Contains(t, report, ":MasonInstall gopls")
	assert.Contains(t, report, "OK nvim 0.12.6")
	assert.Empty(t, errorsOf(t, v))
}

type themePayload struct {
	Name       any      `json:"name" msgpack:"name"`
	Dark       bool     `json:"dark" msgpack:"dark"`
	Background string   `json:"background" msgpack:"background"`
	Foreground string   `json:"foreground" msgpack:"foreground"`
	Palette    []string `json:"palette" msgpack:"palette"`
}

var (
	waveTheme = themePayload{
		Name: "Kanagawa Wave", Dark: true, Background: "#1f1f28", Foreground: "#dcd7ba",
		Palette: []string{
			"#090618", "#c34043", "#76946a", "#c0a36e", "#7e9cd8", "#957fb8", "#6a9589", "#c8c093",
			"#727169", "#e82424", "#98bb6c", "#e6c384", "#7fb4ca", "#938aa9", "#7aa89f", "#dcd7ba",
		},
	}
	lightOwlTheme = themePayload{
		Name: "Light Owl", Dark: false, Background: "#fbfbfb", Foreground: "#403f53",
		Palette: []string{
			"#403f53", "#de3d3b", "#08916a", "#e0af02", "#288ed7", "#d6438a", "#2aa298", "#bdbdbd",
			"#989fb1", "#de3d3b", "#08916a", "#daaa01", "#288ed7", "#d6438a", "#2aa298", "#f0f0f0",
		},
	}
	draculaTheme = themePayload{
		Name: "Dracula", Dark: true, Background: "#282a36", Foreground: "#f8f8f2",
		Palette: []string{
			"#21222c", "#ff5555", "#50fa7b", "#f1fa8c", "#bd93f9", "#ff79c6", "#8be9fd", "#f8f8f2",
			"#6272a4", "#ff6e6e", "#69ff94", "#ffffa5", "#d6acff", "#ff92df", "#a4ffff", "#ffffff",
		},
	}
)

var generatedGroups = []string{
	"Normal", "NormalFloat", "FloatBorder", "FloatTitle", "Pmenu", "PmenuSel", "PmenuSbar", "PmenuThumb",
	"Search", "IncSearch", "CurSearch", "Visual", "CursorLine", "CursorLineNr", "LineNr", "SignColumn",
	"WinSeparator", "StatusLine", "StatusLineNC", "Comment", "Constant", "String", "Character", "Number",
	"Boolean", "Identifier", "Function", "Statement", "Keyword", "Operator", "Type", "Special", "PreProc",
	"Title", "Directory", "Error", "Todo", "DiagnosticError", "DiagnosticWarn", "DiagnosticInfo",
	"DiagnosticHint", "DiagnosticOk", "Added", "Changed", "Removed", "DiffAdd", "DiffChange", "DiffDelete",
	"DiffText", "MatchParen", "NonText", "Whitespace",
}

const hlLua = `
local h = vim.api.nvim_get_hl(0, { name = ..., link = false })
local function hex(c) return c and string.format("#%06x", c) or "" end
return { fg = hex(h.fg), bg = hex(h.bg), defined = next(h) ~= nil }`

type hlColors struct {
	Fg      string `msgpack:"fg"`
	Bg      string `msgpack:"bg"`
	Defined bool   `msgpack:"defined"`
}

func writeTheme(t *testing.T, p themePayload) string {
	t.Helper()
	b, err := json.Marshal(p)
	require.NoError(t, err)
	f := filepath.Join(t.TempDir(), "theme.json")
	require.NoError(t, os.WriteFile(f, b, 0o644))
	return f
}

func hl(t *testing.T, v *nvim.Nvim, group string) hlColors {
	t.Helper()
	var c hlColors
	require.NoError(t, v.ExecLua(hlLua, &c, group))
	return c
}

func colorsName(t *testing.T, v *nvim.Nvim) string {
	t.Helper()
	var name string
	require.NoError(t, v.Eval("get(g:, 'colors_name', '')", &name))
	return name
}

func blendHex(t *testing.T, a, b string, alpha float64) string {
	t.Helper()
	parse := func(s string) [3]float64 {
		n, err := strconv.ParseUint(s[1:], 16, 32)
		require.NoError(t, err)
		return [3]float64{float64(n >> 16 & 0xff), float64(n >> 8 & 0xff), float64(n & 0xff)}
	}
	x, y := parse(a), parse(b)
	var out [3]int
	for i := range out {
		out[i] = int(math.Floor(x[i]*alpha + y[i]*(1-alpha) + 0.5))
	}
	return fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2])
}

func plainKanagawa(t *testing.T, res, group string) hlColors {
	t.Helper()
	v, err := nvim.NewChildProcess(
		nvim.ChildProcessCommand(filepath.Join(res, "nvim", "bin", "nvim")),
		nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-i", "NONE", "--cmd", "set rtp^="+filepath.Join(res, "nvim-distro", "plugins", "kanagawa.nvim")),
		nvim.ChildProcessEnv([]string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin"}),
		nvim.ChildProcessLogf(func(string, ...any) {}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close() })
	require.NoError(t, v.Command("colorscheme kanagawa-wave"))
	return hl(t, v, group)
}

func TestKnownThemeUsesKanagawaSyntax(t *testing.T) {
	v := startDistro(t, opts{ThemeFile: writeTheme(t, waveTheme)})
	assert.Equal(t, "clubmate", colorsName(t, v))
	want := plainKanagawa(t, resources(t), "Keyword")
	require.NotEmpty(t, want.Fg)
	assert.Equal(t, want.Fg, hl(t, v, "Keyword").Fg)
	assert.Equal(t, blendHex(t, waveTheme.Foreground, waveTheme.Background, 0.10), hl(t, v, "FloatBorder").Fg)
	assert.Empty(t, errorsOf(t, v))
}

func TestLightOwlIsLight(t *testing.T) {
	v := startDistro(t, opts{ThemeFile: writeTheme(t, lightOwlTheme)})
	var bg string
	require.NoError(t, v.Eval("&background", &bg))
	assert.Equal(t, "light", bg)
	assert.Equal(t, "#fbfbfb", hl(t, v, "Normal").Bg)
	assert.Equal(t, "clubmate", colorsName(t, v))
	assert.Empty(t, errorsOf(t, v))
}

func TestUnknownThemeGenerated(t *testing.T) {
	v := startDistro(t, opts{ThemeFile: writeTheme(t, draculaTheme)})
	assert.Equal(t, draculaTheme.Palette[2], hl(t, v, "String").Fg)
	assert.Equal(t, draculaTheme.Palette[4], hl(t, v, "Function").Fg)
	for _, g := range generatedGroups {
		assert.True(t, hl(t, v, g).Defined, g)
	}
	assert.Equal(t, "clubmate", colorsName(t, v))
	assert.Empty(t, errorsOf(t, v))
}

func TestApplyLive(t *testing.T) {
	v := startDistro(t, opts{ThemeFile: writeTheme(t, waveTheme)})
	settle(t, v)
	assert.Equal(t, "#1f1f28", hl(t, v, "Normal").Bg)
	require.NoError(t, v.ExecLua(`require("clubmate.theme").apply(...)`, nil, lightOwlTheme))
	assert.Equal(t, "#fbfbfb", hl(t, v, "Normal").Bg)
	assert.Equal(t, lightOwlTheme.Palette[4], hl(t, v, "lualine_a_normal").Bg)
	assert.Equal(t, "clubmate", colorsName(t, v))
	assert.Empty(t, errorsOf(t, v))
}

func TestApplyRespectsOtherColorscheme(t *testing.T) {
	v := startDistro(t, opts{
		ThemeFile: writeTheme(t, waveTheme),
		UserLayer: map[string]string{"lua/user/plugins/x.lua": `return { "folke/snacks.nvim", init = function()
  vim.api.nvim_create_autocmd("User", { pattern = "VeryLazy", once = true, callback = function() vim.cmd.colorscheme("habamax") end })
end }`},
	})
	require.Equal(t, "habamax", colorsName(t, v))
	normal := hl(t, v, "Normal")
	require.NoError(t, v.ExecLua(`require("clubmate.theme").apply(...)`, nil, lightOwlTheme))
	assert.Equal(t, "habamax", colorsName(t, v))
	assert.Equal(t, normal, hl(t, v, "Normal"))
	assert.Empty(t, errorsOf(t, v))
}

func TestNoThemeFileFallsBack(t *testing.T) {
	v := startDistro(t, opts{})
	assert.Equal(t, "clubmate", colorsName(t, v))
	assert.Equal(t, "#1f1f28", hl(t, v, "Normal").Bg)
	assert.Empty(t, errorsOf(t, v))
}

func TestUi2Enabled(t *testing.T) {
	v := startDistro(t, opts{})
	var state struct {
		Cmd       bool
		Win       bool
		Cmdheight int
	}
	require.NoError(t, v.ExecLua(`
local ui2 = require("vim._core.ui2")
return { Cmd = type(ui2.cmd) == "table", Win = vim.api.nvim_win_is_valid(ui2.wins.cmd), Cmdheight = vim.o.cmdheight }`, &state))
	assert.True(t, state.Cmd)
	assert.True(t, state.Win)
	assert.Equal(t, 0, state.Cmdheight)
}

func waitMode(t *testing.T, v *nvim.Nvim, want string) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		m, err := v.Mode()
		require.NoError(t, err)
		return m.Mode == want
	}, 5*time.Second)
}

func TestCmdlinePopup(t *testing.T) {
	v := startDistro(t, opts{})
	_, err := v.Input(":")
	require.NoError(t, err)
	waitMode(t, v, "c")
	var cfg struct {
		Relative string
		Bordered bool
		Title    string
		Width    int
		Columns  int
	}
	require.NoError(t, v.ExecLua(`
local c = vim.api.nvim_win_get_config(require("vim._core.ui2").wins.cmd)
local title = type(c.title) == "table" and table.concat(vim.tbl_map(function(chunk) return chunk[1] end, c.title)) or tostring(c.title or "")
return { Relative = c.relative, Bordered = c.border ~= nil and c.border ~= "none", Title = title, Width = c.width, Columns = vim.o.columns }`, &cfg))
	_, err = v.Input("<Esc>")
	require.NoError(t, err)
	waitMode(t, v, "n")
	assert.Equal(t, "editor", cfg.Relative)
	assert.True(t, cfg.Bordered)
	assert.Contains(t, cfg.Title, "Cmdline")
	assert.Equal(t, cfg.Columns/2, cfg.Width)
	assert.Empty(t, errorsOf(t, v))
}

func TestWhichKeyLeaderGroup(t *testing.T) {
	v := startDistro(t, opts{})
	var desc string
	require.NoError(t, v.ExecLua(`
local node = require("which-key.buf").get({ mode = "n", update = true }).tree:find("<leader>")
return node and node.mapping and node.mapping.desc or ""`, &desc))
	assert.Equal(t, "Leader", desc)
}

func TestWhichKeyLeaderGroupsNamed(t *testing.T) {
	v := startDistro(t, opts{})
	settle(t, v)
	var bad []string
	require.NoError(t, v.ExecLua(`
local view = require("which-key.view")
local leader = require("which-key.buf").get({ mode = "n", update = true }).tree:find("<leader>")
local bad = {}
for _, child in ipairs(leader:children()) do
  if child:is_group() then
    local icon = view.icon(child)
    if not child.desc or child.desc == "" or not icon or icon == "" then
      bad[#bad + 1] = child.key
    end
  end
end
return bad`, &bad))
	assert.Empty(t, bad)
}

func TestNotifyIsSnacks(t *testing.T) {
	v := startDistro(t, opts{})
	var got struct {
		Notify bool
		Select bool
	}
	require.NoError(t, v.ExecLua(`
vim.notify("probe")
return { Notify = vim.notify == Snacks.notifier.notify, Select = vim.ui.select == Snacks.picker.select }`, &got))
	assert.True(t, got.Notify)
	assert.True(t, got.Select)
}

func TestBundledAppSmoke(t *testing.T) {
	if os.Getenv("CLUBMATE_TEST_BUNDLE") == "" {
		t.Skip("CLUBMATE_TEST_BUNDLE is not set; scripts/bundle-app.sh runs this against the built app")
	}
	for _, c := range []struct {
		name string
		test func(*testing.T)
	}{
		{"VendoredPluginsComplete", TestVendoredPluginsComplete},
		{"StartsClean", TestStartsClean},
		{"OfflineReadOnly", TestOfflineReadOnly},
		{"LongInstallPath", TestLongInstallPath},
		{"ParsersLoadAndHighlight", TestParsersLoadAndHighlight},
		{"BlinkUsesRust", TestBlinkUsesRust},
		{"CheckhealthClean", TestCheckhealthClean},
	} {
		t.Run(c.name, c.test)
	}
}

func TestLicencesMatchLock(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "THIRD_PARTY_LICENSES.md"))
	require.NoError(t, err)
	licences := string(b)
	f, err := os.Open(filepath.Join("..", "scripts", "nvim-distro.lock"))
	require.NoError(t, err)
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) != 5 {
			continue
		}
		heading, ok := map[string]string{
			"plugin":    "### %s %s",
			"parser":    "### %s tree-sitter parser %s",
			"blink-lib": "#### Rust crates in libblink_cmp_fuzzy.dylib (%s %s)",
		}[fields[0]]
		if !ok {
			continue
		}
		want := fmt.Sprintf(heading, fields[1], fields[2])
		assert.True(t, strings.Contains(licences, "\n"+want+"\n"), "THIRD_PARTY_LICENSES.md lacks %q; regenerate with scripts/licenses.sh", want)
	}
	require.NoError(t, s.Err())
}
