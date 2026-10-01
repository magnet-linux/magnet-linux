#!/usr/bin/env python3
"""Serve a local browser check and record its JavaScript callback."""
import http.server
import pathlib
import sys
import urllib.parse

from audio_fixture import tone

work = pathlib.Path(sys.argv[1])
page = b"""<!doctype html>
<meta charset="utf-8"><title>Magnet browser check</title>
<style>body{font:24px sans-serif;background:#17222c;color:#eee;margin:4em}
h1{color:#87d3b0}canvas{border:1px solid #87d3b0}</style>
<h1>Browser on Magnet Linux</h1><p id="result">Running JavaScript...</p>
<canvas width="440" height="100"></canvas>
<script>
const ctx = document.querySelector('canvas').getContext('2d');
ctx.fillStyle = '#87d3b0'; ctx.fillRect(20,20,400,60);
ctx.fillStyle = '#17222c'; ctx.font = '24px sans-serif';
ctx.fillText('Canvas rendering works',45,60);
requestAnimationFrame(() => requestAnimationFrame(async () => {
  const response = await fetch('/passed', {cache:'no-store'});
  if (!response.ok) throw new Error('Callback failed');
  document.querySelector('#result').textContent =
    'HTML, JavaScript, fetch and Canvas 2D passed.';
  document.title = 'Magnet browser check passed';
}));
</script>"""


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        request = urllib.parse.urlparse(self.path)
        query = urllib.parse.parse_qs(request.query)
        if request.path == '/tone.wav':
            frequency = int(query.get('frequency', ['440'])[0])
            if frequency not in (220, 330, 440, 660, 880):
                self.send_error(400)
                return
            body, kind = tone(frequency), 'audio/wav'
        elif request.path in ('/audio', '/audio-passed'):
            browser = query.get('browser', [''])[0]
            if browser not in ('firefox', 'chromium'):
                self.send_error(400)
                return
            if request.path == '/audio-passed':
                (work / f'{browser}-audio-passed').touch()
                body, kind = b'OK', 'text/plain'
            else:
                frequency = 660 if browser == 'firefox' else 880
                body = f'''<!doctype html><meta charset="utf-8">
                <title>Magnet audio check</title>
                <style>body{{background:#17222c;color:white;font:24px sans-serif}}
                button{{position:fixed;inset:0;background:#17222c;color:#87d3b0;
                border:0;font:32px sans-serif}}</style>
                <button id="play">Play {browser} audio test</button>
                <script>
                const audio = new Audio('/tone.wav?frequency={frequency}');
                document.querySelector('#play').onclick = async () => {{
                  const button = document.querySelector('#play');
                  button.textContent = 'Starting audio...';
                  try {{
                    await audio.play();
                    button.textContent = 'Playing {frequency} Hz';
                  }} catch (error) {{
                    button.textContent = 'Playback failed: ' + error.message;
                  }}
                }};
                audio.onended = async () => {{
                  await fetch('/audio-passed?browser={browser}');
                  document.querySelector('#play').textContent = 'Audio playback completed';
                }};
                </script>'''.encode()
                kind = 'text/html; charset=utf-8'
        elif self.path == '/passed':
            (work / 'javascript-passed').touch()
            body, kind = b'OK', 'text/plain'
        else:
            body, kind = page, 'text/html; charset=utf-8'
        self.send_response(200)
        self.send_header('Content-Type', kind)
        self.send_header('Cache-Control', 'no-store')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)


server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
(work / 'http-port').write_text(str(server.server_port))
server.serve_forever()
