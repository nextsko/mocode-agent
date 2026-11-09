const http = require('http');
const port = Number(process.argv[2] || 18765);
function reply(res, id, result) {
  res.writeHead(200, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ jsonrpc: '2.0', id, result }));
}
function error(res, id, code, message) {
  res.writeHead(200, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ jsonrpc: '2.0', id, error: { code, message } }));
}
const srv = http.createServer((req, res) => {
  if (req.method !== 'POST') { res.writeHead(405); res.end(); return; }
  let body = '';
  req.on('data', c => body += c);
  req.on('end', () => {
    let m; try { m = JSON.parse(body); } catch { res.writeHead(400); res.end(); return; }
    if (m.method === 'initialize') {
      reply(res, m.id, { protocolVersion: '2024-11-05', capabilities: { tools: {} }, serverInfo: { name: 'http-echo', version: '1.0.0' } });
    } else if (m.method === 'tools/list') {
      reply(res, m.id, { tools: [{ name: 'http_echo', description: 'Echo via HTTP', inputSchema: { type: 'object', properties: { text: { type: 'string' } }, required: ['text'] } }] });
    } else if (m.method === 'tools/call') {
      const text = (m.params && m.params.arguments && m.params.arguments.text) || '';
      reply(res, m.id, { content: [{ type: 'text', text: 'HTTP_ECHO:' + text }] });
    } else if (m.id !== undefined) {
      reply(res, m.id, {});
    } else {
      res.writeHead(202); res.end();
    }
  });
});
srv.listen(port, '127.0.0.1', () => console.log('http-mcp listening ' + port));
