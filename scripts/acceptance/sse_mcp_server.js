const http = require('http');
const port = Number(process.argv[2] || 18766);
let clientRes = null;
const pending = []; // {id,payload}
function push(obj) {
  const data = 'event: message\ndata: ' + JSON.stringify(obj) + '\n\n';
  if (clientRes) clientRes.write(data); else pending.push(data);
}
function handle(m) {
  if (m.method === 'initialize') {
    push({ jsonrpc: '2.0', id: m.id, result: { protocolVersion: '2024-11-05', capabilities: { tools: {} }, serverInfo: { name: 'sse-echo', version: '1.0.0' } } });
  } else if (m.method === 'tools/list') {
    push({ jsonrpc: '2.0', id: m.id, result: { tools: [{ name: 'sse_echo', description: 'Echo via SSE', inputSchema: { type: 'object', properties: { text: { type: 'string' } }, required: ['text'] } }] } });
  } else if (m.method === 'tools/call') {
    const text = (m.params && m.params.arguments && m.params.arguments.text) || '';
    push({ jsonrpc: '2.0', id: m.id, result: { content: [{ type: 'text', text: 'SSE_ECHO:' + text }] } });
  } else if (m.id !== undefined) {
    push({ jsonrpc: '2.0', id: m.id, result: {} });
  }
}
const srv = http.createServer((req, res) => {
  const u = new URL(req.url, 'http://127.0.0.1');
  if (req.method === 'GET' && u.pathname === '/sse') {
    res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' });
    res.write('event: endpoint\ndata: /message?sessionId=abc\n\n');
    clientRes = res;
    while (pending.length) res.write(pending.shift());
    req.on('close', () => { clientRes = null; });
    return;
  }
  if (req.method === 'POST' && u.pathname === '/message') {
    let body = '';
    req.on('data', c => body += c);
    req.on('end', () => { try { handle(JSON.parse(body)); } catch {} res.writeHead(202); res.end(); });
    return;
  }
  res.writeHead(404); res.end();
});
srv.listen(port, '127.0.0.1', () => console.log('sse-mcp listening ' + port));
