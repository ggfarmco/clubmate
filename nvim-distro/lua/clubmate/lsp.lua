local M = {}

M.servers = {
  gopls = "gopls",
  rust_analyzer = "rust-analyzer",
  ts_ls = "typescript-language-server",
  tailwindcss = "tailwindcss-language-server",
  sqls = "sqls",
  solidity_ls_nomicfoundation = "nomicfoundation-solidity-language-server",
}

local function names()
  local out = vim.tbl_keys(M.servers)
  table.sort(out)
  return out
end

local function state_file()
  return vim.fn.stdpath("state") .. "/clubmate/lsp-notified"
end

local function notified()
  local seen = {}
  local f = io.open(state_file(), "r")
  if f then
    for line in f:lines() do
      seen[line] = true
    end
    f:close()
  end
  return seen
end

local function remember(name)
  local path = state_file()
  vim.fn.mkdir(vim.fs.dirname(path), "p")
  local f = io.open(path, "a")
  if f then
    f:write(name, "\n")
    f:close()
  end
end

local function command(name)
  local cfg = vim.lsp.config[name]
  local cmd = cfg and cfg.cmd
  if type(cmd) == "table" and type(cmd[1]) == "string" then
    return cmd[1]
  end
end

local function installed(name)
  local cmd = command(name)
  return cmd == nil or vim.fn.executable(cmd) == 1
end

function M.notice(name)
  return name .. " is not installed. Run :MasonInstall " .. M.servers[name]
end

function M.missing()
  local out = {}
  for _, name in ipairs(names()) do
    if not installed(name) then
      out[#out + 1] = { name = name, cmd = command(name), package = M.servers[name] }
    end
  end
  return out
end

local function on_filetype(args)
  local ft = vim.bo[args.buf].filetype
  local seen
  for _, name in ipairs(names()) do
    local cfg = vim.lsp.config[name]
    if cfg and vim.tbl_contains(cfg.filetypes or {}, ft) and not installed(name) then
      seen = seen or notified()
      if not seen[name] then
        seen[name] = true
        remember(name)
        vim.notify(M.notice(name), vim.log.levels.WARN)
      end
    end
  end
end

function M.setup()
  local ok, blink = pcall(require, "blink.cmp")
  if ok then
    vim.lsp.config("*", { capabilities = blink.get_lsp_capabilities() })
  end
  vim.lsp.enable(names())
  vim.lsp.inlay_hint.enable(false)
  vim.diagnostic.config({ virtual_text = true, severity_sort = true })
  vim.api.nvim_create_autocmd("FileType", {
    group = vim.api.nvim_create_augroup("ClubmateLspNotice", { clear = true }),
    callback = on_filetype,
  })
end

return M
