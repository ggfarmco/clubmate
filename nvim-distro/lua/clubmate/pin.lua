local root = vim.fs.dirname(vim.fs.dirname(vim.fs.dirname(vim.fs.normalize(debug.getinfo(1, "S").source:sub(2)))))
local manifest = dofile(root .. "/lua/clubmate/manifest.lua")

return setmetatable({ root = root, manifest = manifest }, {
  __call = function(_, repo, spec)
    local name = repo:match("[^/]+$")
    if not manifest.plugins[name] then
      error("clubmate.pin: " .. repo .. " is not in the manifest")
    end
    return vim.tbl_extend("force", spec or {}, { repo, name = name, dir = root .. "/plugins/" .. name })
  end,
})
