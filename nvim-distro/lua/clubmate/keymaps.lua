local map = vim.keymap.set

local function pick(source, opts)
  return function()
    Snacks.picker[source](opts)
  end
end

map("n", "<leader>ff", pick("files"), { desc = "Find files" })
map("n", "<leader>fg", pick("grep"), { desc = "Live grep" })
map("n", "<leader>fb", pick("buffers"), { desc = "Buffers" })
map("n", "<leader>fh", pick("help"), { desc = "Help tags" })
map("n", "<leader>fr", pick("lsp_references"), { desc = "LSP references" })
map("n", "<leader>fs", pick("lsp_symbols"), { desc = "Document symbols" })
map("n", "<leader>fi", pick("lsp_implementations"), { desc = "LSP implementations" })
map("n", "<leader>fd", pick("diagnostics_buffer"), { desc = "Document diagnostics" })
map("n", "<leader><leader>", pick("resume"), { desc = "Resume last search" })
map("n", "<leader>fy", pick("registers"), { desc = "Registers (clipboard)" })

map("n", "<leader>gt", function()
  local file = vim.fn.expand("%")
  local target
  if file:match("_test%.go$") then
    target = file:gsub("_test%.go$", ".go")
  elseif file:match("%.go$") then
    target = file:gsub("%.go$", "_test.go")
  end
  if target and vim.fn.filereadable(target) == 1 then
    vim.cmd("edit " .. target)
  else
    vim.notify("Test file not found: " .. (target or ""), vim.log.levels.WARN)
  end
end, { desc = "Go to test file" })

map("n", "<leader>lg", "<cmd>LazyGit<cr>", { desc = "LazyGit" })
map("n", "<leader>lf", "<cmd>LazyGitFilterCurrentFile<cr>", { desc = "LazyGit file history" })

map("n", "gd", vim.lsp.buf.definition, { desc = "Go to definition" })
map("n", "gD", vim.lsp.buf.declaration, { desc = "Go to declaration" })
map("n", "gl", vim.diagnostic.open_float, { desc = "Show diagnostic" })
map("n", "gr", vim.lsp.buf.rename, { desc = "Rename symbol" })

local function jump_diag(count)
  return function()
    vim.diagnostic.jump({
      count = count,
      on_jump = function(_, bufnr)
        vim.diagnostic.open_float({ bufnr = bufnr })
      end,
    })
  end
end
map("n", "[d", jump_diag(-1), { desc = "Prev diagnostic" })
map("n", "]d", jump_diag(1), { desc = "Next diagnostic" })

map("n", "<Esc>", "<cmd>nohlsearch<cr>", { desc = "Clear search highlight" })
map("n", "<leader>q", "<cmd>cclose<cr>", { desc = "Close quickfix" })

map("n", "<C-h>", "<C-w>h", { desc = "Move to left window" })
map("n", "<C-j>", "<C-w>j", { desc = "Move to bottom window" })
map("n", "<C-k>", "<C-w>k", { desc = "Move to top window" })
map("n", "<C-l>", "<C-w>l", { desc = "Move to right window" })

map("v", "<", "<gv", { desc = "Indent left and reselect" })
map("v", ">", ">gv", { desc = "Indent right and reselect" })

map("v", "J", ":m '>+1<CR>gv=gv", { desc = "Move selection down" })
map("v", "K", ":m '<-2<CR>gv=gv", { desc = "Move selection up" })

map("n", "<leader>ns", function()
  vim.cmd("enew")
  vim.bo.buftype = "nofile"
  vim.bo.bufhidden = "hide"
  vim.bo.swapfile = false
  vim.bo.filetype = "markdown"
end, { desc = "New scratch buffer" })

map("n", "<leader>bd", "<cmd>bd<cr>", { desc = "Close buffer" })

map("n", "<leader>cp", function()
  local path = vim.fn.expand("%:.")
  vim.fn.setreg("+", path)
  vim.notify(path)
end, { desc = "Copy file path" })

local function open_doc()
  local file = vim.api.nvim_buf_get_name(0)
  local ext = file:lower():match("%.(%w+)$")
  if ext ~= "md" and ext ~= "markdown" and ext ~= "mmd" and ext ~= "html" and ext ~= "htm" then
    vim.notify("Not a Markdown, mermaid or HTML file", vim.log.levels.WARN)
    return
  end
  local m = package.loaded["clubmate"]
  if type(m) ~= "table" or not m.chan then
    vim.notify("Not running in Clubmate", vim.log.levels.WARN)
    return
  end
  vim.rpcnotify(m.chan, "clubmate", { event = "docOpen", file = file })
end

vim.api.nvim_create_user_command("ClubmateDocOpen", open_doc, { desc = "Open doc in viewer" })
map("n", "<leader>mp", open_doc, { desc = "Open doc in viewer" })

map("n", "<leader>cb", "<cmd>!go build ./...<cr>", { desc = "Go build" })
map("n", "<leader>cc", "<cmd>!go test -c ./...<cr>", { desc = "Go test compile" })
map("n", "<leader>ci", function()
  vim.lsp.inlay_hint.enable(not vim.lsp.inlay_hint.is_enabled())
end, { desc = "Toggle inlay hints" })

map("n", "<Space>", "za", { desc = "Toggle fold under cursor" })
map("n", "zV", function()
  if vim.o.foldlevel > 0 then
    vim.cmd("normal! zM")
  else
    vim.cmd("normal! zR")
  end
end, { desc = "Toggle all folds" })

map("n", "<leader>ss", "<cmd>set spell!<cr>", { desc = "Toggle spell" })
map("n", "<leader>sn", "]s", { desc = "Next spelling error" })
map("n", "<leader>sp", "[s", { desc = "Prev spelling error" })
map("n", "<leader>sa", "zg", { desc = "Add to dictionary" })
map("n", "<leader>sf", "z=", { desc = "Fix spelling" })
