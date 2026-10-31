local augroup = vim.api.nvim_create_augroup
local autocmd = vim.api.nvim_create_autocmd

augroup("FormatOnSave", { clear = true })
autocmd("BufWritePre", {
  group = "FormatOnSave",
  pattern = { "*.go" },
  callback = function(args)
    for _, client in ipairs(vim.lsp.get_clients({ bufnr = args.buf, name = "gopls" })) do
      local params = vim.lsp.util.make_range_params(0, client.offset_encoding)
      params.context = { only = { "source.organizeImports" } }
      local resp = client:request_sync("textDocument/codeAction", params, 1000, args.buf)
      local actions = resp and resp.result
      if type(actions) == "table" then
        for _, action in pairs(actions) do
          if action.edit then
            vim.lsp.util.apply_workspace_edit(action.edit, client.offset_encoding)
          elseif action.command then
            client:exec_cmd(action.command, { bufnr = args.buf })
          end
        end
      end
    end
    vim.lsp.buf.format({ bufnr = args.buf, async = false })
  end,
})

autocmd("BufWritePre", {
  group = "FormatOnSave",
  pattern = { "*.rs" },
  callback = function(args)
    vim.lsp.buf.format({ bufnr = args.buf, async = false })
  end,
})

augroup("GoSettings", { clear = true })
autocmd("FileType", {
  group = "GoSettings",
  pattern = "go",
  callback = function()
    vim.opt_local.tabstop = 4
    vim.opt_local.shiftwidth = 4
    vim.opt_local.expandtab = false
  end,
})

augroup("HighlightYank", { clear = true })
autocmd("TextYankPost", {
  group = "HighlightYank",
  callback = function()
    vim.hl.on_yank({ timeout = 200 })
  end,
})

augroup("QuickfixMappings", { clear = true })
autocmd("FileType", {
  group = "QuickfixMappings",
  pattern = "qf",
  callback = function()
    vim.keymap.set("n", "v", "<C-w><CR><C-w>L", { buffer = true, desc = "Open in vertical split" })
    vim.keymap.set("n", "s", "<C-w><CR>", { buffer = true, desc = "Open in horizontal split" })
  end,
})

augroup("LspDocumentHighlight", { clear = true })
autocmd({ "CursorHold", "CursorHoldI" }, {
  group = "LspDocumentHighlight",
  callback = function()
    local clients = vim.lsp.get_clients({ bufnr = 0 })
    for _, client in ipairs(clients) do
      if client:supports_method("textDocument/documentHighlight") then
        vim.lsp.buf.document_highlight()
        return
      end
    end
  end,
})
autocmd({ "CursorMoved", "CursorMovedI" }, {
  group = "LspDocumentHighlight",
  callback = function()
    vim.lsp.buf.clear_references()
  end,
})

augroup("SpellCheck", { clear = true })
autocmd("FileType", {
  group = "SpellCheck",
  pattern = { "markdown", "text", "gitcommit" },
  callback = function()
    vim.opt_local.spell = true
  end,
})

augroup("CleanStaleSwap", { clear = true })
autocmd("VimEnter", {
  group = "CleanStaleSwap",
  callback = function()
    vim.schedule(function()
      local swapdir = vim.fn.stdpath("state") .. "/swap"
      if vim.fn.isdirectory(swapdir) == 0 then
        return
      end

      local function orphaned(pid)
        if not pid or pid <= 0 then
          return true
        end
        local ok, _, errname = vim.uv.kill(pid, 0)
        return ok ~= 0 and errname == "ESRCH"
      end

      local host = vim.fn.hostname()
      for name, ftype in vim.fs.dir(swapdir) do
        if ftype == "file" then
          local path = swapdir .. "/" .. name
          local info = vim.fn.swapinfo(path)
          if type(info) == "table" and not info.error and info.host == host
            and info.dirty == 0 and orphaned(info.pid) then
            vim.fn.delete(path)
          end
        end
      end
    end)
  end,
})

