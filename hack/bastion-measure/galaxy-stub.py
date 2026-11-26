"""A Bastion's own refusal of a Galaxy read, as engine.writeShaped renders it in the galaxy shape (R-BAS-095); a measurement of
W34-E15, run once (docs/developer/bastion.md, "The galaxy shape against ansible-galaxy 2.17"). Not a test, never in the product.

Answers the API root of the repository with the Galaxy's discovery document and the collection document of e4.app with 200, and every
other request (the version list first) with the status and code given on the command line, its Retry-After and the galaxy shape's
body, byte for byte what TestR_BAS_095_EachShapeRenders holds for a 429. Logs every request to stderr. Run inside the client's
container, which has python3:

    galaxy-stub.py 429 rate_limited     # or: 503 rate_limited, 503 busy
"""
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

STATUS = int(sys.argv[1])
CODE = sys.argv[2]
PHRASE = {429: "Too Many Requests", 503: "Service Unavailable", 403: "Forbidden"}[STATUS]
DETAIL = {
    "rate_limited": "Too many requests from this address. Try again in 14 minutes.",
    "busy": "Too many requests are waiting; try again in a moment.",
    "read_only": "This address is read-only: it serves downloads and accepts no uploads.",
}[CODE]
ROOT = "/api/ansible/e4/galaxy/"


class H(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        sys.stderr.write("stub: %s %s\n" % (self.command, self.path))

    def answer(self):
        if self.command == "GET" and self.path.split("?")[0] == ROOT:
            body = b'{"available_versions":{"v3":"v3/"},"current_version":"v3"}'
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
        elif self.command == "GET" and self.path.split("?")[0] == ROOT + "v3/collections/e4/app/":
            # The collection's document is served, as it was before the Bastion's limit refused the version list.
            body = json.dumps({"namespace": "e4", "name": "app", "created_at": "2026-11-26T10:00:00Z",
                               "updated_at": "2026-11-26T10:00:00Z", "deprecated": False,
                               "href": ROOT + "v3/collections/e4/app/",
                               "versions_url": ROOT + "v3/collections/e4/app/versions/",
                               "highest_version": {"href": ROOT + "v3/collections/e4/app/versions/1.0.0/", "version": "1.0.0"}}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
        else:
            body = json.dumps({"errors": [{"status": str(STATUS), "code": CODE, "title": PHRASE + ".", "detail": DETAIL}]},
                              separators=(",", ":")).encode()
            self.send_response(STATUS)
            self.send_header("Content-Type", "application/json")
            self.send_header("Cache-Control", "no-store")
            self.send_header("X-Content-Type-Options", "nosniff")
            if STATUS in (429, 503):
                self.send_header("Retry-After", "1" if CODE == "busy" else "30")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    do_GET = answer
    do_POST = answer


HTTPServer(("127.0.0.1", 8080), H).serve_forever()
