// Mock native Claude upstream that cuts the stream WITHOUT message_delta/message_stop.
const http = require('http');

const server = http.createServer((req, res) => {
  let body = '';
  req.on('data', (d) => (body += d));
  req.on('end', () => {
    console.log(`[mock-claude] ${req.method} ${req.url}`);
    res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' });
    const events = [
      'event: message_start\ndata: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"glm-5.3","content":[],"usage":{"input_tokens":8,"output_tokens":1}}}\n\n',
      'event: content_block_start\ndata: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}\n\n',
      'event: content_block_delta\ndata: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}\n\n',
      // NOTE: stream cut here -- no content_block_stop, no message_delta, no message_stop
    ];
    let i = 0;
    const timer = setInterval(() => {
      if (i < events.length) {
        res.write(events[i]);
        i++;
      } else {
        clearInterval(timer);
        res.end(); // hard close
      }
    }, 50);
  });
});

server.listen(9912, () => console.log('mock claude upstream on :9912'));
