// Mock upstream simulating doubao-agent-plan behavior:
// streams content chunks then [DONE] WITHOUT any finish_reason chunk or usage.
const http = require('http');

const server = http.createServer((req, res) => {
  let body = '';
  req.on('data', (d) => (body += d));
  req.on('end', () => {
    console.log(`[mock] ${req.method} ${req.url}`);
    res.writeHead(200, {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
    });
    const chunks = [
      { id: 'chatcmpl-1', object: 'chat.completion.chunk', created: 1700000000, model: 'glm-5.3', choices: [{ index: 0, delta: { role: 'assistant', content: 'Hello' }, finish_reason: null }] },
      { id: 'chatcmpl-1', object: 'chat.completion.chunk', created: 1700000000, model: 'glm-5.3', choices: [{ index: 0, delta: { content: ' world' }, finish_reason: null }] },
      // NOTE: no finish_reason chunk, no usage chunk -- directly [DONE]
    ];
    let i = 0;
    const timer = setInterval(() => {
      if (i < chunks.length) {
        res.write('data: ' + JSON.stringify(chunks[i]) + '\n\n');
        i++;
      } else {
        res.write('data: [DONE]\n\n');
        clearInterval(timer);
        res.end();
      }
    }, 50);
  });
});

server.listen(9911, () => console.log('mock upstream on :9911'));
