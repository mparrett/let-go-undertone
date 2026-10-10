#!/usr/bin/env python3
# LAN dev server for the undertone web demo: binds all interfaces and sends
# Cache-Control: no-cache so phones always revalidate index.html + main.wasm
# (heuristic caching of a stale wasm made new flags silently kill the binary).
import http.server, os, sys

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 8899
DIR = sys.argv[2] if len(sys.argv) > 2 else "web"
os.chdir(os.path.join(os.path.dirname(os.path.abspath(__file__)), DIR))

class H(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Cache-Control", "no-cache")
        super().end_headers()

http.server.ThreadingHTTPServer.allow_reuse_address = True
print(f"serving web/ on 0.0.0.0:{PORT}")
http.server.ThreadingHTTPServer(("0.0.0.0", PORT), H).serve_forever()
