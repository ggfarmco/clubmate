vim.cmd("hi clear")
if vim.fn.exists("syntax_on") then
  vim.cmd("syntax reset")
end
vim.o.termguicolors = true
vim.g.colors_name = "lightowl"

local c = {
  bg = "#FBFBFB",
  fg = "#403F53",
  cursor = "#403F53",
  selection = "#E0E0E0",
  selection_inactive = "#EDEDED",
  search = "#D6EDFF",
  comment = "#989FB1",
  red = "#DE3D3B",
  green = "#08916A",
  yellow = "#E0AF02",
  blue = "#288ED7",
  magenta = "#D6438A",
  cyan = "#2AA298",
  white = "#F0F0F0",
  cursor_line = "#F0F0F0",
  pmenu = "#F3F3F3",
  border = "#D9D9D9",
}

local hl = function(group, opts)
  vim.api.nvim_set_hl(0, group, opts)
end

hl("Normal", { fg = c.fg, bg = c.bg })
hl("NormalFloat", { fg = c.fg, bg = c.pmenu })
hl("FloatBorder", { fg = c.border, bg = c.pmenu })
hl("Cursor", { fg = c.bg, bg = c.cursor })
hl("CursorLine", { bg = c.cursor_line })
hl("CursorLineNr", { fg = c.magenta, bg = c.cursor_line, bold = true })
hl("LineNr", { fg = c.comment })
hl("SignColumn", { bg = c.bg })
hl("VertSplit", { fg = c.border })
hl("WinSeparator", { fg = c.border })
hl("StatusLine", { fg = c.fg, bg = c.selection })
hl("StatusLineNC", { fg = c.comment, bg = c.selection_inactive })
hl("TabLine", { fg = c.comment, bg = c.selection_inactive })
hl("TabLineFill", { bg = c.selection_inactive })
hl("TabLineSel", { fg = c.fg, bg = c.bg, bold = true })

hl("Visual", { bg = c.search })
hl("Search", { fg = c.fg, bg = c.search })
hl("IncSearch", { fg = c.bg, bg = c.blue })
hl("CurSearch", { fg = c.bg, bg = c.blue })

hl("Pmenu", { fg = c.fg, bg = c.pmenu })
hl("PmenuSel", { fg = c.fg, bg = c.selection })
hl("PmenuSbar", { bg = c.selection_inactive })
hl("PmenuThumb", { bg = c.comment })

hl("Comment", { fg = c.comment, italic = true })
hl("Constant", { fg = c.red })
hl("String", { fg = c.green })
hl("Character", { fg = c.green })
hl("Number", { fg = c.magenta })
hl("Boolean", { fg = c.red })
hl("Float", { fg = c.magenta })

hl("Identifier", { fg = c.fg })
hl("Function", { fg = c.blue })

hl("Statement", { fg = c.magenta })
hl("Conditional", { fg = c.magenta })
hl("Repeat", { fg = c.magenta })
hl("Label", { fg = c.magenta })
hl("Operator", { fg = c.magenta })
hl("Keyword", { fg = c.magenta })
hl("Exception", { fg = c.magenta })

hl("PreProc", { fg = c.magenta })
hl("Include", { fg = c.magenta })
hl("Define", { fg = c.magenta })
hl("Macro", { fg = c.magenta })
hl("PreCondit", { fg = c.magenta })

hl("Type", { fg = c.cyan })
hl("StorageClass", { fg = c.magenta })
hl("Structure", { fg = c.cyan })
hl("Typedef", { fg = c.cyan })

hl("Special", { fg = c.blue })
hl("SpecialChar", { fg = c.red })
hl("Tag", { fg = c.blue })
hl("Delimiter", { fg = c.fg })
hl("SpecialComment", { fg = c.comment })
hl("Debug", { fg = c.red })

hl("Underlined", { underline = true })
hl("Error", { fg = c.red })
hl("Todo", { fg = c.magenta, bold = true })

hl("DiagnosticError", { fg = c.red })
hl("DiagnosticWarn", { fg = c.yellow })
hl("DiagnosticInfo", { fg = c.blue })
hl("DiagnosticHint", { fg = c.cyan })
hl("DiagnosticUnderlineError", { undercurl = true, sp = c.red })
hl("DiagnosticUnderlineWarn", { undercurl = true, sp = c.yellow })
hl("DiagnosticUnderlineInfo", { undercurl = true, sp = c.blue })
hl("DiagnosticUnderlineHint", { undercurl = true, sp = c.cyan })

hl("DiffAdd", { bg = "#D6EFD6" })
hl("DiffChange", { bg = "#E5E5FF" })
hl("DiffDelete", { fg = c.red, bg = "#FFE5E5" })
hl("DiffText", { bg = "#C5C5FF" })

hl("GitSignsAdd", { fg = c.green })
hl("GitSignsChange", { fg = c.blue })
hl("GitSignsDelete", { fg = c.red })

hl("MatchParen", { bg = c.selection, bold = true })
hl("NonText", { fg = c.border })
hl("SpecialKey", { fg = c.border })
hl("Whitespace", { fg = c.border })
hl("EndOfBuffer", { fg = c.bg })

hl("Directory", { fg = c.blue })
hl("Title", { fg = c.magenta, bold = true })
hl("Question", { fg = c.blue })
hl("MoreMsg", { fg = c.cyan })
hl("WarningMsg", { fg = c.yellow })
hl("ErrorMsg", { fg = c.red })

hl("@variable", { fg = c.fg })
hl("@variable.builtin", { fg = c.cyan })
hl("@variable.parameter", { fg = c.fg })
hl("@constant", { fg = c.red })
hl("@constant.builtin", { fg = c.red })
hl("@module", { fg = c.cyan })
hl("@string", { fg = c.green })
hl("@string.escape", { fg = c.red })
hl("@character", { fg = c.green })
hl("@number", { fg = c.magenta })
hl("@boolean", { fg = c.red })
hl("@float", { fg = c.magenta })
hl("@function", { fg = c.blue })
hl("@function.builtin", { fg = c.blue })
hl("@function.call", { fg = c.blue })
hl("@function.method", { fg = c.blue })
hl("@function.method.call", { fg = c.blue })
hl("@constructor", { fg = c.cyan })
hl("@keyword", { fg = c.magenta })
hl("@keyword.function", { fg = c.magenta })
hl("@keyword.operator", { fg = c.magenta })
hl("@keyword.return", { fg = c.magenta })
hl("@keyword.import", { fg = c.magenta })
hl("@operator", { fg = c.magenta })
hl("@punctuation", { fg = c.fg })
hl("@punctuation.bracket", { fg = c.fg })
hl("@punctuation.delimiter", { fg = c.fg })
hl("@type", { fg = c.cyan })
hl("@type.builtin", { fg = c.cyan })
hl("@tag", { fg = c.blue })
hl("@tag.attribute", { fg = c.cyan })
hl("@tag.delimiter", { fg = c.fg })
hl("@comment", { fg = c.comment, italic = true })

hl("FzfLuaNormal", { fg = c.fg, bg = c.bg })
hl("FzfLuaBorder", { fg = c.border, bg = c.bg })
hl("FzfLuaTitle", { fg = c.magenta, bg = c.bg, bold = true })
hl("FzfLuaPreviewNormal", { fg = c.fg, bg = c.bg })
hl("FzfLuaPreviewBorder", { fg = c.border, bg = c.bg })
hl("FzfLuaPreviewTitle", { fg = c.blue, bg = c.bg, bold = true })
hl("FzfLuaCursor", { fg = c.bg, bg = c.cursor })
hl("FzfLuaCursorLine", { bg = c.selection })
hl("FzfLuaSearch", { fg = c.bg, bg = c.blue })

hl("TelescopeNormal", { fg = c.fg, bg = c.bg })
hl("TelescopeBorder", { fg = c.border, bg = c.bg })
hl("TelescopePromptNormal", { fg = c.fg, bg = c.bg })
hl("TelescopePromptBorder", { fg = c.border, bg = c.bg })
hl("TelescopeResultsNormal", { fg = c.fg, bg = c.bg })
hl("TelescopeResultsBorder", { fg = c.border, bg = c.bg })
hl("TelescopePreviewNormal", { fg = c.fg, bg = c.bg })
hl("TelescopePreviewBorder", { fg = c.border, bg = c.bg })

hl("NeoTreeNormal", { fg = c.fg, bg = c.bg })
hl("NeoTreeNormalNC", { fg = c.fg, bg = c.bg })
hl("NeoTreeEndOfBuffer", { fg = c.bg, bg = c.bg })
hl("NeoTreeDirectoryIcon", { fg = c.blue })
hl("NeoTreeDirectoryName", { fg = c.blue })
hl("NeoTreeFileName", { fg = c.fg })
hl("NeoTreeGitAdded", { fg = c.green })
hl("NeoTreeGitModified", { fg = c.blue })
hl("NeoTreeGitDeleted", { fg = c.red })

hl("WhichKey", { fg = c.magenta })
hl("WhichKeyGroup", { fg = c.blue })
hl("WhichKeyDesc", { fg = c.fg })
hl("WhichKeySeperator", { fg = c.comment })
hl("WhichKeyFloat", { bg = c.bg })
hl("WhichKeyBorder", { fg = c.border, bg = c.bg })

hl("LazyNormal", { fg = c.fg, bg = c.bg })
hl("LazyButton", { fg = c.fg, bg = c.selection })
hl("LazyButtonActive", { fg = c.bg, bg = c.blue })

hl("MasonNormal", { fg = c.fg, bg = c.bg })
