import http from 'node:http';
import { readFile } from 'node:fs/promises';
import { resolve, extname, sep } from 'node:path';

const root = resolve(import.meta.dirname, '../site/dist');
const types = { '.html': 'text/html; charset=utf-8', '.css': 'text/css; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.svg': 'image/svg+xml', '.ttf': 'font/ttf', '.txt': 'text/plain; charset=utf-8', '.zip': 'application/zip', '.gz': 'application/gzip' };

async function serve(request, response) {
  try {
    const url = new URL(request.url, 'http://localhost');
    const file = resolve(root, '.' + decodeURIComponent(url.pathname === '/' ? '/index.html' : url.pathname));
    if (!file.startsWith(root + sep)) {
      response.writeHead(403).end('Forbidden');
      return;
    }
    const body = await readFile(file);
    response.writeHead(200, { 'Content-Type': types[extname(file)] ?? 'application/octet-stream', 'Cache-Control': 'no-cache' }).end(body);
  } catch {
    response.writeHead(404).end('Not found');
  }
}

const server = http.createServer(serve);
const port = Number(process.argv[2] ?? 4173);
server.listen(port, '127.0.0.1', () => process.stdout.write(`ResetWatch preview: http://127.0.0.1:${port}\n`));
process.on('SIGINT', () => server.close());
process.on('SIGTERM', () => server.close());
