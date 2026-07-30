// 本机前端静态服务（模拟生产：80 前端 + 3000 接口）
// SPA fallback 只针对页面路由；/api 等一律反代到 127.0.0.1:3000
const http = require('http');
const fs = require('fs');
const path = require('path');

const DIST = path.join(__dirname, '..', 'web', 'classic', 'dist');
const API_TARGET = { host: '127.0.0.1', port: 3000 };

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.ttf': 'font/ttf',
  '.txt': 'text/plain; charset=utf-8',
};

// 需要反代到后端的路径前缀（API、OAuth 回调、支付回调等）
const PROXY_PREFIXES = ['/api/', '/oauth/', '/api'];

function shouldProxy(urlPath) {
  return PROXY_PREFIXES.some(
    (p) => urlPath === p || urlPath.startsWith(p),
  );
}

function proxyToBackend(req, res) {
  const options = {
    host: API_TARGET.host,
    port: API_TARGET.port,
    path: req.url,
    method: req.method,
    headers: { ...req.headers, host: `${API_TARGET.host}:${API_TARGET.port}` },
  };
  const upstream = http.request(options, (upRes) => {
    res.writeHead(upRes.statusCode, upRes.headers);
    upRes.pipe(res);
  });
  upstream.on('error', (err) => {
    res.writeHead(502, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ success: false, message: 'backend unavailable: ' + err.message }));
  });
  req.pipe(upstream);
}

const server = http.createServer((req, res) => {
  const urlPath = decodeURIComponent(req.url.split('?')[0]);

  if (shouldProxy(urlPath)) {
    return proxyToBackend(req, res);
  }

  // 静态文件；不存在则 SPA fallback 到 index.html
  let filePath = path.join(DIST, path.normalize(urlPath).replace(/^(\.\.[\/\\])+/, ''));
  if (!filePath.startsWith(DIST)) {
    res.writeHead(403);
    return res.end('forbidden');
  }
  fs.stat(filePath, (err, st) => {
    if (err || !st.isFile()) {
      filePath = path.join(DIST, 'index.html');
    }
    const ext = path.extname(filePath).toLowerCase();
    res.writeHead(200, {
      'Content-Type': MIME[ext] || 'application/octet-stream',
      'Cache-Control': ext === '.html' ? 'no-cache' : 'public, max-age=604800',
    });
    fs.createReadStream(filePath).pipe(res);
  });
});

server.listen(80, () => {
  console.log('front: http://localhost:80 (static dist + /api proxy -> 127.0.0.1:3000)');
});
