return {
  cmd = { "gopls" },
  filetypes = { "go", "gomod", "gowork", "gotmpl" },
  root_dir = function(bufnr, on_dir)
    local name = vim.api.nvim_buf_get_name(bufnr)
    if name:match("^%a[%w+.-]*://") then
      return
    end
    local root = vim.fs.root(bufnr, { "go.mod", "go.work", ".git" })
    if root then
      on_dir(root)
    end
  end,
  settings = {
    gopls = {
      gofumpt = false,
      staticcheck = true,
      usePlaceholders = true,
      analyses = {
        unusedparams = true,
        shadow = true,
        ST1000 = false,
        ST1003 = false,
      },
      hints = {
        assignVariableTypes = true,
        compositeLiteralFields = true,
        constantValues = true,
        functionTypeParameters = true,
        parameterNames = true,
        rangeVariableTypes = true,
      },
    },
  },
}
