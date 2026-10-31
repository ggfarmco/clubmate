local M = {}

M.known = {
  ["Kanagawa Wave"] = "kanagawa-wave",
  ["Kanagawa Dragon"] = "kanagawa-dragon",
  ["Kanagawa Lotus"] = "kanagawa-lotus",
  ["Light Owl"] = "lightowl",
}

M.default = {
  name = "Kanagawa Wave",
  dark = true,
  background = "#1f1f28",
  foreground = "#dcd7ba",
  palette = {
    "#090618", "#c34043", "#76946a", "#c0a36e", "#7e9cd8", "#957fb8", "#6a9589", "#c8c093",
    "#727169", "#e82424", "#98bb6c", "#e6c384", "#7fb4ca", "#938aa9", "#7aa89f", "#dcd7ba",
  },
}

local function is_hex(s)
  return type(s) == "string" and s:match("^#%x%x%x%x%x%x$") ~= nil
end

function M.valid(t)
  if type(t) ~= "table" or not is_hex(t.background) or not is_hex(t.foreground) then
    return false
  end
  if type(t.palette) ~= "table" or #t.palette ~= 16 then
    return false
  end
  for _, c in ipairs(t.palette) do
    if not is_hex(c) then
      return false
    end
  end
  return true
end

local function rgb(s)
  local n = tonumber(s:sub(2), 16)
  return math.floor(n / 65536) % 256, math.floor(n / 256) % 256, n % 256
end

function M.blend(a, b, alpha)
  local ar, ag, ab = rgb(a)
  local br, bg, bb = rgb(b)
  local function mix(x, y)
    return math.floor(x * alpha + y * (1 - alpha) + 0.5)
  end
  return string.format("#%02x%02x%02x", mix(ar, br), mix(ag, bg), mix(ab, bb))
end

function M.tokens(t)
  t = t or vim.g.clubmate_theme
  if not M.valid(t) then
    t = M.default
  end
  local p, fg, bg = t.palette, t.foreground, t.background
  return {
    name = type(t.name) == "string" and t.name or nil,
    dark = t.dark ~= false,
    palette = p,
    fg = fg,
    bg = bg,
    border = M.blend(fg, bg, 0.10),
    muted = M.blend(fg, bg, 0.55),
    comment = M.blend(fg, bg, 0.45),
    faint = M.blend(fg, bg, 0.25),
    surface = M.blend(fg, bg, 0.06),
    selection = M.blend(p[5], bg, 0.25),
    accent = p[5],
    success = p[3],
    warning = p[4],
    danger = p[2],
  }
end

local function diff(c)
  return {
    DiffAdd = { bg = M.blend(c.success, c.bg, 0.2) },
    DiffChange = { bg = M.blend(c.warning, c.bg, 0.2) },
    DiffDelete = { bg = M.blend(c.danger, c.bg, 0.2) },
    DiffText = { bg = M.blend(c.warning, c.bg, 0.35) },
  }
end

function M.generate(t)
  local c = M.tokens(t)
  local p = c.palette
  local groups = {
    Normal = { fg = c.fg, bg = c.bg },
    NormalFloat = { fg = c.fg, bg = c.bg },
    FloatBorder = { fg = c.border, bg = c.bg },
    FloatTitle = { fg = c.accent, bg = c.bg, bold = true },
    Pmenu = { fg = c.fg, bg = c.surface },
    PmenuSel = { bg = c.selection },
    PmenuSbar = { bg = c.surface },
    PmenuThumb = { bg = c.muted },
    Search = { fg = c.fg, bg = M.blend(p[4], c.bg, 0.35) },
    IncSearch = { fg = c.bg, bg = p[4] },
    CurSearch = { fg = c.bg, bg = p[4] },
    Visual = { bg = c.selection },
    CursorLine = { bg = c.surface },
    CursorLineNr = { fg = p[4], bold = true },
    LineNr = { fg = c.comment },
    SignColumn = { fg = c.muted },
    WinSeparator = { fg = c.border },
    StatusLine = { fg = c.muted, bg = c.surface },
    StatusLineNC = { fg = c.comment, bg = c.bg },
    Comment = { fg = c.comment, italic = true },
    Constant = { fg = p[7] },
    String = { fg = p[3] },
    Character = { fg = p[3] },
    Number = { fg = p[7] },
    Boolean = { fg = p[7] },
    Identifier = { fg = c.fg },
    Function = { fg = p[5] },
    Statement = { fg = p[6] },
    Keyword = { fg = p[6] },
    Operator = { fg = c.muted },
    Type = { fg = p[4] },
    Special = { fg = p[13] },
    PreProc = { fg = p[14] },
    Title = { fg = p[5], bold = true },
    Directory = { fg = p[5] },
    Error = { fg = p[2] },
    Todo = { fg = c.bg, bg = p[4], bold = true },
    DiagnosticError = { fg = p[2] },
    DiagnosticWarn = { fg = p[4] },
    DiagnosticInfo = { fg = p[5] },
    DiagnosticHint = { fg = p[7] },
    DiagnosticOk = { fg = p[3] },
    Added = { fg = p[3] },
    Changed = { fg = p[4] },
    Removed = { fg = p[2] },
    MatchParen = { fg = p[12], bold = true },
    NonText = { fg = c.faint },
    Whitespace = { fg = c.faint },
    ["@variable"] = { link = "Identifier" },
    ["@variable.builtin"] = { link = "Special" },
    ["@function.builtin"] = { link = "Special" },
    ["@constructor"] = { link = "Type" },
    ["@module"] = { link = "Type" },
    ["@property"] = { link = "Identifier" },
    ["@tag"] = { link = "Keyword" },
    ["@markup.heading"] = { link = "Title" },
    ["@markup.link"] = { link = "Special" },
  }
  return vim.tbl_extend("force", groups, diff(c))
end

function M.chrome(t)
  local c = M.tokens(t)
  local g = {
    NormalFloat = { fg = c.fg, bg = c.bg },
    FloatBorder = { fg = c.border, bg = c.bg },
    FloatTitle = { fg = c.accent, bg = c.bg, bold = true },
    FloatFooter = { fg = c.muted, bg = c.bg },
    Pmenu = { fg = c.fg, bg = c.bg },
    PmenuSel = { bg = c.selection },
    PmenuSbar = { bg = c.surface },
    PmenuThumb = { bg = c.muted },
    PmenuMatch = { fg = c.accent, bold = true },
    WinSeparator = { fg = c.border },
    StatusLine = { fg = c.muted, bg = c.bg },
    StatusLineNC = { fg = c.muted, bg = c.bg },
    MsgArea = { fg = c.fg, bg = c.bg },
    GitSignsAdd = { fg = c.success },
    GitSignsChange = { fg = c.warning },
    GitSignsDelete = { fg = c.danger },
    WhichKey = { fg = c.accent },
    WhichKeyGroup = { fg = c.palette[6] },
    WhichKeyDesc = { fg = c.fg },
    WhichKeySeparator = { fg = c.muted },
    WhichKeyValue = { fg = c.muted },
    WhichKeyNormal = { link = "NormalFloat" },
    WhichKeyBorder = { link = "FloatBorder" },
    WhichKeyTitle = { link = "FloatTitle" },
    SnacksPickerMatch = { fg = c.accent, bold = true },
    SnacksPickerPrompt = { fg = c.accent },
    SnacksPickerDir = { fg = c.muted },
    SnacksPickerListCursorLine = { bg = c.selection },
    SnacksInputNormal = { link = "NormalFloat" },
    SnacksInputBorder = { link = "FloatBorder" },
    SnacksInputTitle = { link = "FloatTitle" },
    SnacksInputIcon = { fg = c.accent },
    BlinkCmpMenu = { link = "Pmenu" },
    BlinkCmpMenuBorder = { link = "FloatBorder" },
    BlinkCmpMenuSelection = { link = "PmenuSel" },
    BlinkCmpDoc = { link = "NormalFloat" },
    BlinkCmpDocBorder = { link = "FloatBorder" },
    BlinkCmpDocSeparator = { link = "FloatBorder" },
    BlinkCmpSignatureHelp = { link = "NormalFloat" },
    BlinkCmpSignatureHelpBorder = { link = "FloatBorder" },
    BlinkCmpLabelMatch = { fg = c.accent, bold = true },
    BlinkCmpScrollBarThumb = { link = "PmenuThumb" },
    BlinkCmpScrollBarGutter = { link = "PmenuSbar" },
    DiffviewNormal = { link = "Normal" },
    DiffviewWinSeparator = { link = "WinSeparator" },
    DiffviewFilePanelTitle = { fg = c.accent, bold = true },
    DiffviewFilePanelCounter = { fg = c.muted },
    DiffviewFilePanelRootPath = { fg = c.muted },
    DiffviewFilePanelInsertions = { fg = c.success },
    DiffviewFilePanelDeletions = { fg = c.danger },
    DiffviewStatusAdded = { fg = c.success },
    DiffviewStatusUntracked = { fg = c.success },
    DiffviewStatusModified = { fg = c.warning },
    DiffviewStatusRenamed = { fg = c.warning },
    DiffviewStatusDeleted = { fg = c.danger },
    NeoTreeNormal = { link = "Normal" },
    NeoTreeNormalNC = { link = "Normal" },
    NeoTreeWinSeparator = { link = "WinSeparator" },
    NeoTreeFloatBorder = { link = "FloatBorder" },
    NeoTreeFloatTitle = { link = "FloatTitle" },
    NeoTreeTitleBar = { fg = c.bg, bg = c.accent },
    NeoTreeRootName = { fg = c.accent, bold = true },
    NeoTreeDirectoryIcon = { fg = c.accent },
    NeoTreeGitAdded = { fg = c.success },
    NeoTreeGitUntracked = { fg = c.success },
    NeoTreeGitModified = { fg = c.warning },
    NeoTreeGitDeleted = { fg = c.danger },
    NeoTreeGitConflict = { fg = c.danger },
  }
  for _, part in ipairs({ "", "Input", "List", "Preview", "Box" }) do
    g["SnacksPicker" .. part] = { link = "NormalFloat" }
    g["SnacksPicker" .. part .. "Border"] = { link = "FloatBorder" }
    g["SnacksPicker" .. part .. "Title"] = { link = "FloatTitle" }
  end
  for level, color in pairs({ Error = c.danger, Warn = c.warning, Info = c.accent, Debug = c.muted, Trace = c.muted }) do
    g["SnacksNotifier" .. level] = { fg = c.fg, bg = c.bg }
    g["SnacksNotifierBorder" .. level] = { fg = M.blend(color, c.bg, 0.5), bg = c.bg }
    g["SnacksNotifierTitle" .. level] = { fg = color, bg = c.bg }
    g["SnacksNotifierIcon" .. level] = { fg = color }
  end
  return vim.tbl_extend("force", g, diff(c))
end

function M.lualine()
  local c = M.tokens()
  local function mode(color)
    return {
      a = { fg = c.bg, bg = color, gui = "bold" },
      b = { fg = c.fg, bg = c.surface },
      c = { fg = c.muted, bg = c.bg },
    }
  end
  return {
    normal = mode(c.accent),
    insert = mode(c.success),
    visual = mode(c.palette[6]),
    replace = mode(c.danger),
    command = mode(c.warning),
    inactive = {
      a = { fg = c.muted, bg = c.bg },
      b = { fg = c.muted, bg = c.bg },
      c = { fg = c.muted, bg = c.bg },
    },
  }
end

local function set(groups)
  for name, spec in pairs(groups) do
    vim.api.nvim_set_hl(0, name, spec)
  end
end

local function syntax(c)
  local scheme = M.known[c.name or ""]
  if scheme == "lightowl" then
    vim.cmd.runtime("colors/lightowl.lua")
    return
  end
  local variant = scheme and scheme:match("^kanagawa%-(%a+)$")
  local ok, kanagawa = pcall(require, "kanagawa")
  if variant and ok then
    kanagawa.setup({ compile = false, colors = { theme = { all = { ui = { bg_gutter = "none" } } } } })
    kanagawa.load(variant)
    return
  end
  vim.cmd("hi clear")
  if vim.fn.exists("syntax_on") == 1 then
    vim.cmd("syntax reset")
  end
  set(M.generate(vim.g.clubmate_theme))
end

function M.load()
  local c = M.tokens()
  local background = c.dark and "dark" or "light"
  if vim.o.background ~= background then
    vim.o.background = background
  end
  vim.o.termguicolors = true
  syntax(c)
  set(M.chrome(vim.g.clubmate_theme))
  for i, color in ipairs(c.palette) do
    vim.g["terminal_color_" .. (i - 1)] = color
  end
  vim.g.colors_name = "clubmate"
end

function M.read(path)
  if not path or path == "" then
    return nil
  end
  local ok, lines = pcall(vim.fn.readfile, path)
  if not ok then
    return nil
  end
  local decoded, t = pcall(vim.json.decode, table.concat(lines, "\n"), { luanil = { object = true, array = true } })
  if decoded and M.valid(t) then
    return t
  end
end

function M.startup()
  vim.g.clubmate_theme = M.read(vim.env.CLUBMATE_THEME_FILE) or M.default
  vim.cmd.colorscheme("clubmate")
end

function M.apply(t)
  if not M.valid(t) then
    return
  end
  vim.g.clubmate_theme = t
  if vim.g.colors_name == "clubmate" then
    vim.cmd.colorscheme("clubmate")
  end
end

return M
