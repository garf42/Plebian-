"""Probe: one Chromium driven by an agent (accessibility snapshot + clicks) while
unsafe requests (RFC 9110: not GET/HEAD/OPTIONS/TRACE) that follow an agent action
are paused as drafts; the owner can drive the same page through screencast + input
events; cookies can be read back. Run twice; outputs must match except timings."""
import json, threading, time, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from playwright.sync_api import sync_playwright

POSTS = []
PAGE = b"""<!doctype html><html><body>
<h1>Feed store</h1>
<form method=get action=/search><input name=q aria-label=Search><button>Search</button></form>
<form method=post action=/order><label>Qty <input name=qty value=1></label><button>Place order</button></form>
<a href=/login>Sign in</a>
<script>setTimeout(()=>navigator.sendBeacon('/beacon','x'),50)</script>
</body></html>"""
LOGIN = b"""<!doctype html><html><body><form method=post action=/session>
<input name=user aria-label=User><input name=pass type=password aria-label=Password><button>Sign in</button></form></body></html>"""

class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        body = LOGIN if self.path.startswith('/login') else PAGE
        self.send_response(200); self.send_header('content-type', 'text/html'); self.end_headers(); self.wfile.write(body)
    def do_POST(self):
        n = int(self.headers.get('content-length', 0)); data = self.rfile.read(n).decode()
        POSTS.append((self.path, data))
        self.send_response(303)
        if self.path == '/session':
            self.send_header('set-cookie', 'sid=abc123; HttpOnly; Path=/')
        self.send_header('location', '/'); self.end_headers()

srv = ThreadingHTTPServer(('127.0.0.1', 0), H); port = srv.server_address[1]
threading.Thread(target=srv.serve_forever, daemon=True).start()
base = f'http://127.0.0.1:{port}'
out = {}
with sync_playwright() as p:
    b = p.chromium.launch(channel='chrome', headless=True)
    ctx = b.new_context(); page = ctx.new_page()
    cdp = ctx.new_cdp_session(page)
    page.goto(base + '/'); page.wait_for_timeout(200)
    snap = page.locator('body').aria_snapshot()
    out['snapshot_chars'] = len(snap)
    out['snapshot_has_order_button'] = 'Place order' in snap
    # hold unsafe requests issued while an agent action settles
    held = []
    cdp.send('Fetch.enable', {'patterns': [{'urlPattern': '*', 'requestStage': 'Request'}]})
    def on_paused(ev):
        m = ev['request']['method']
        if m in ('GET', 'HEAD', 'OPTIONS', 'TRACE'):
            cdp.send('Fetch.continueRequest', {'requestId': ev['requestId']})
        else:
            held.append(ev)
    cdp.on('Fetch.requestPaused', on_paused)
    beacon_before = len([x for x in POSTS if x[0] == '/beacon'])
    page.get_by_role('button', name='Place order').click(no_wait_after=True)
    page.wait_for_timeout(500)
    out['held_after_click'] = [(e['request']['method'], e['request']['url'].replace(base, ''), e['request'].get('postData')) for e in held]
    out['server_posts_while_held'] = [x for x in POSTS if x[0] == '/order']
    # owner approves the draft: release exactly the held bytes
    for e in held:
        cdp.send('Fetch.continueRequest', {'requestId': e['requestId']})
    page.wait_for_timeout(500)
    out['server_order_posts_after_approval'] = [x for x in POSTS if x[0] == '/order']
    cdp.send('Fetch.disable')
    # owner takes the wheel: screencast frames + raw input events on the same page
    frames = []
    cdp.on('Page.screencastFrame', lambda ev: (frames.append(len(ev['data'])), cdp.send('Page.screencastFrameAck', {'sessionId': ev['sessionId']})))
    cdp.send('Page.startScreencast', {'format': 'jpeg', 'quality': 60, 'everyNthFrame': 1})
    page.goto(base + '/login'); page.wait_for_timeout(300)
    box = page.get_by_label('User').bounding_box()
    x, y = box['x'] + 5, box['y'] + 5
    for t in ('mousePressed', 'mouseReleased'):
        cdp.send('Input.dispatchMouseEvent', {'type': t, 'x': x, 'y': y, 'button': 'left', 'clickCount': 1})
    cdp.send('Input.insertText', {'text': 'owner'})
    page.get_by_label('Password').click(); cdp.send('Input.insertText', {'text': 's3cret'})
    page.keyboard.press('Enter'); page.wait_for_timeout(500)
    cdp.send('Page.stopScreencast')
    out['screencast_frames'] = len(frames) > 0
    out['session_post_body_contains_password'] = any('s3cret' in d for pth, d in POSTS if pth == '/session')
    out['cookie_names'] = sorted(c['name'] for c in ctx.cookies())
    b.close()
srv.shutdown()
print(json.dumps(out, sort_keys=True))
