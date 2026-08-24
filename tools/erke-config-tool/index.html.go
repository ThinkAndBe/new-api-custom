//go:build windows

package main

// indexHTML 内嵌的操作界面：粘贴配置链接 → 选客户端 → 一键配置。
const indexHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>ERKE AI 配置工具</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: "Microsoft YaHei", system-ui, sans-serif; background: #f5f7fa; min-height: 100vh; display: flex; align-items: center; justify-content: center; }
  .card { background: #fff; border-radius: 16px; box-shadow: 0 8px 32px rgba(0,0,0,.08); padding: 40px 44px; width: 480px; max-width: 94vw; }
  h1 { font-size: 20px; color: #1c1f23; margin-bottom: 4px; }
  .sub { font-size: 12px; color: #8a919f; margin-bottom: 24px; }
  label { display: block; font-size: 13px; color: #4e5561; margin: 14px 0 6px; font-weight: 600; }
  input[type=text] { width: 100%; padding: 10px 12px; border: 1px solid #d9dee5; border-radius: 8px; font-size: 13px; outline: none; }
  input[type=text]:focus { border-color: #3b82f6; }
  .radios { display: flex; gap: 10px; margin-top: 6px; }
  .radio { flex: 1; border: 1.5px solid #d9dee5; border-radius: 8px; padding: 10px; text-align: center; cursor: pointer; font-size: 14px; color: #4e5561; }
  .radio.on { border-color: #3b82f6; color: #1d64dd; background: #eff6ff; font-weight: 600; }
  button { width: 100%; margin-top: 22px; padding: 12px; background: #2563eb; color: #fff; border: 0; border-radius: 8px; font-size: 15px; font-weight: 600; cursor: pointer; }
  button:hover { background: #1d4ed8; }
  button:disabled { background: #93b4f5; cursor: not-allowed; }
  .msg { margin-top: 16px; padding: 12px; border-radius: 8px; font-size: 13px; line-height: 1.6; display: none; }
  .msg.ok { display: block; background: #f0fdf4; color: #15803d; border: 1px solid #bbf7d0; }
  .msg.err { display: block; background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca; }
  .tip { margin-top: 14px; font-size: 11.5px; color: #a0a7b3; line-height: 1.7; }
</style>
</head>
<body>
<div class="card">
  <h1>ERKE AI 配置工具</h1>
  <div class="sub">一键配置 WorkBuddy / CodeBuddy 的模型</div>

  <label>配置链接（在使用教程页点「复制链接」）</label>
  <input type="text" id="url" placeholder="粘贴 https://tokenhub... 开头的链接">

  <label>配置到哪个客户端</label>
  <div class="radios">
    <div class="radio on" data-p="workbuddy" onclick="pick(this)">WorkBuddy</div>
    <div class="radio" data-p="codebuddy" onclick="pick(this)">CodeBuddy</div>
  </div>

  <button id="btn" onclick="apply()">一键配置</button>
  <div class="msg" id="msg"></div>

  <div class="tip">
    · 配置会写入用户目录下的 .workbuddy / .codebuddy 文件夹（models.json）<br>
    · 完成后重启对应客户端即可使用；模型更新后重新运行一次本工具即可
  </div>
</div>
<script>
var product = 'workbuddy';
function pick(el) {
  document.querySelectorAll('.radio').forEach(function (r) { r.classList.remove('on'); });
  el.classList.add('on');
  product = el.dataset.p;
}
function apply() {
  var url = document.getElementById('url').value.trim();
  var msg = document.getElementById('msg');
  var btn = document.getElementById('btn');
  if (!url) { msg.className = 'msg err'; msg.textContent = '请先粘贴配置链接'; return; }
  btn.disabled = true; btn.textContent = '配置中…';
  fetch('/apply', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: url, product: product })
  }).then(function (r) { return r.json(); }).then(function (d) {
    msg.className = 'msg ' + (d.success ? 'ok' : 'err');
    msg.textContent = d.message;
    btn.disabled = false; btn.textContent = '一键配置';
  }).catch(function (e) {
    msg.className = 'msg err'; msg.textContent = '请求失败：' + e;
    btn.disabled = false; btn.textContent = '一键配置';
  });
}
</script>
</body>
</html>`
