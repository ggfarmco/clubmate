#!/usr/bin/env bash
SPARKLE_VERSION="2.10.1"
SPARKLE_SHA256="4c2e48a3e50ebcc2c88a58a4c7264a1a99c3c31857ce754357afb9cdb323022a"
SPARKLE_ZIP_URL="https://github.com/sparkle-project/Sparkle/releases/download/$SPARKLE_VERSION/Sparkle-for-Swift-Package-Manager.zip"

sparkle_bin() {
  local spm="$ROOT/build/DerivedData/SourcePackages/artifacts/sparkle/Sparkle/bin"
  local cache="$ROOT/build/sparkle-$SPARKLE_VERSION"
  if [[ -x "$spm/sign_update" && -x "$spm/BinaryDelta" ]]; then
    echo "$spm"
    return
  fi
  if [[ ! -x "$cache/bin/sign_update" || ! -x "$cache/bin/BinaryDelta" ]]; then
    local tmp
    tmp="$(mktemp -d)"
    curl -fsSL -o "$tmp/sparkle.zip" "$SPARKLE_ZIP_URL"
    echo "$SPARKLE_SHA256  $tmp/sparkle.zip" | shasum -a 256 -c - >&2
    rm -rf "$cache"
    ditto -x -k "$tmp/sparkle.zip" "$cache"
    rm -rf "$tmp"
  fi
  echo "$cache/bin"
}

tree_digest() {
  (
    cd "$1" || exit 1
    find . -print0 | sort -z | xargs -0 stat -f '%N %HT %Lp %Y'
    find . -type f -print0 | sort -z | xargs -0 shasum -a 256
    /usr/bin/python3 -I -c '
import ctypes, hashlib, os
libc = ctypes.CDLL(None, use_errno=True)
NOFOLLOW = 1
skip = {b"com.apple.provenance", b"com.apple.quarantine", b"com.apple.macl", b"com.apple.lastuseddate#PS"}
def names(p):
    n = libc.listxattr(p, None, 0, NOFOLLOW)
    if n < 0:
        raise OSError(ctypes.get_errno(), p)
    buf = ctypes.create_string_buffer(n)
    libc.listxattr(p, buf, n, NOFOLLOW)
    return sorted(set(x for x in buf.raw.split(b"\0") if x) - skip)
def value(p, a):
    n = libc.getxattr(p, a, None, 0, 0, NOFOLLOW)
    buf = ctypes.create_string_buffer(max(n, 0))
    libc.getxattr(p, a, buf, n, 0, NOFOLLOW)
    return buf.raw
for root, dirs, files in os.walk("."):
    dirs.sort()
    for p in [root] + [os.path.join(root, f) for f in sorted(files)] + [os.path.join(root, d) for d in dirs if os.path.islink(os.path.join(root, d))]:
        b = os.fsencode(p)
        for a in names(b):
            print(p, a.decode(), hashlib.sha256(value(b, a)).hexdigest())
'
  )
}
