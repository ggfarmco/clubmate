import json, socket, sys
sock_path, method, params = sys.argv[1], sys.argv[2], json.loads(sys.argv[3])
s = socket.socket(socket.AF_UNIX); s.connect(sock_path)
s.sendall(json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode())
buf, dec = "", json.JSONDecoder()
while True:
    chunk = s.recv(65536)
    if not chunk: break
    buf += chunk.decode()
    while buf.strip():
        try: obj, i = dec.raw_decode(buf.lstrip())
        except ValueError: break
        buf = buf.lstrip()[i:]
        if obj.get("id") == 1:
            print(json.dumps(obj.get("result", obj.get("error")), indent=1)); sys.exit(0 if "result" in obj else 1)
