import React, { useState, useEffect, useCallback } from 'react';
import {
  Card,
  Spin,
  Typography,
  Button,
  Tag,
  Banner,
  Empty,
  Select,
  Divider,
} from '@douyinfe/semi-ui';
import { Download, Terminal, Check } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';
import { useContext } from 'react';
import { StatusContext } from '../../context/Status';

const { Title, Text, Paragraph } = Typography;

// 模型参数表
// 数据来源: 七牛云 AI 模型广场 https://www.qiniu.com/ai/models (model_constraints + architecture)
// 七牛聚合了 DeepSeek/智谱/Qwen/Kimi/MiniMax/豆包等原厂数据，比 OpenRouter 更准确
const MODEL_PARAMS = {
  // ========== 智谱 GLM ==========
  'glm-5.2':         { in: 1000000, out: 128000, tools: true,  vision: false, reasoning: true  },
  'glm-5.1':         { in: 200000,  out: 128000, tools: true,  vision: false, reasoning: false },
  'glm-5':           { in: 200000,  out: 128000, tools: true,  vision: false, reasoning: true  },
  'glm-4.7':         { in: 200000,  out: 200000, tools: true,  vision: false, reasoning: true  },
  'glm-4.7-flash':   { in: 200000,  out: 200000, tools: true,  vision: false, reasoning: true  },
  'glm-4.6':         { in: 200000,  out: 200000, tools: true,  vision: false, reasoning: true  },
  'glm-4.5':         { in: 131072,  out: 98304,  tools: true,  vision: false, reasoning: false },
  // ========== DeepSeek ==========
  'deepseek-v4-pro':     { in: 1000000, out: 384000, tools: true, vision: false, reasoning: true  },
  'deepseek-v4-flash':   { in: 1000000, out: 384000, tools: true, vision: false, reasoning: true  },
  'deepseek-v3.2':       { in: 128000,  out: 32000,  tools: true, vision: false, reasoning: false },
  'deepseek-v3.1':       { in: 128000,  out: 32000,  tools: true, vision: false, reasoning: false },
  'deepseek-v3':         { in: 128000,  out: 16000,  tools: true, vision: false, reasoning: false },
  'deepseek-r1':         { in: 128000,  out: 32000,  tools: true, vision: false, reasoning: true  },
  // ========== 阿里 Qwen ==========
  'qwen3.7-max':         { in: 1000000, out: 65536,  tools: true,  vision: false, reasoning: true  },
  'qwen3.6-plus':        { in: 1000000, out: 65536,  tools: true,  vision: true,  reasoning: true  },
  'qwen3.6-27b':         { in: 262100,  out: 262100, tools: true,  vision: true,  reasoning: true  },
  'qwen3.5-plus':        { in: 1000000, out: 65536,  tools: true,  vision: true,  reasoning: true  },
  'qwen3-max':           { in: 262144,  out: 65536,  tools: true,  vision: false, reasoning: true  },
  'qwen3-coder-plus':    { in: 262000,  out: 65536,  tools: true,  vision: false, reasoning: true  },
  'qwen3-coder':         { in: 262000,  out: 65536,  tools: true,  vision: false, reasoning: true  },
  'qwen3-plus':          { in: 1000000, out: 65536,  tools: true,  vision: false, reasoning: true  },
  'qwen-turbo':          { in: 1000000, out: 8192,   tools: true,  vision: false, reasoning: true  },
  'qwen-plus':           { in: 1000000, out: 65536,  tools: true,  vision: false, reasoning: true  },
  'qwen3-235b':          { in: 128000,  out: 32000,  tools: true,  vision: false, reasoning: true  },
  'qwen3-32b':           { in: 131072,  out: 32768,  tools: true,  vision: false, reasoning: true  },
  // ========== Kimi ==========
  'kimi-k2.7-code':      { in: 262144,  out: 262144, tools: true, vision: true,  reasoning: true  },
  'kimi-k2.6':           { in: 262000,  out: 262000, tools: true, vision: true,  reasoning: true  },
  'kimi-k2.5':           { in: 256000,  out: 256000, tools: true, vision: true,  reasoning: true  },
  'kimi-k2-thinking':    { in: 256000,  out: 100000, tools: true, vision: false, reasoning: true  },
  'kimi-k2':             { in: 128000,  out: 128000, tools: true, vision: false, reasoning: false },
  // ========== MiniMax ==========
  'minimax-m3':          { in: 1000000, out: 128000, tools: true, vision: true,  reasoning: true  },
  'minimax-m2.7':        { in: 204800,  out: 128000, tools: true, vision: false, reasoning: true  },
  'minimax-m2.5':        { in: 204800,  out: 128000, tools: true, vision: false, reasoning: true  },
  'minimax-m2.1':        { in: 204800,  out: 128000, tools: true, vision: false, reasoning: true  },
  'minimax-m2':          { in: 200000,  out: 128000, tools: true, vision: false, reasoning: true  },
  // ========== 豆包 Doubao ==========
  'doubao-seed-2.0-pro': { in: 256000,  out: 128000, tools: true, vision: true,  reasoning: true  },
  'doubao-seed-2.0-code':{ in: 256000,  out: 128000, tools: true, vision: true,  reasoning: true  },
  'doubao-seed-2.0-mini':{ in: 256000,  out: 32000,  tools: true, vision: true,  reasoning: true  },
  'doubao-seed-1.6':     { in: 256000,  out: 32000,  tools: true, vision: true,  reasoning: true  },
  'doubao-1.5-vision':   { in: 128000,  out: 16000,  tools: false, vision: true, reasoning: false },
  'doubao-1.5-thinking': { in: 128000,  out: 16000,  tools: true, vision: false, reasoning: true  },
  // ========== 其他 ==========
  'hy3-preview':         { in: 262144,  out: 262144, tools: true, vision: false, reasoning: true  },
  'longcat-flash':       { in: 256000,  out: 320000, tools: true, vision: false, reasoning: true  },
};

// 根据七牛数据匹配模型参数
function getModelParams(modelName) {
  // 去掉厂商前缀（z-ai/, deepseek/, moonshotai/, minimax/, qwen/, bytedance/ 等）
  const stripped = modelName.replace(/^[a-z]+\//i, '').toLowerCase();
  // 精确匹配
  if (MODEL_PARAMS[stripped]) return MODEL_PARAMS[stripped];
  // 前缀匹配
  for (const key of Object.keys(MODEL_PARAMS)) {
    if (stripped.startsWith(key) || stripped.includes(key)) {
      return MODEL_PARAMS[key];
    }
  }
  // 兜底
  return { in: 128000, out: 8192, tools: true, vision: false, reasoning: false };
}

const UsageGuide = () => {
  const { t } = useTranslation();
  const [tokens, setTokens] = useState([]);
  const [selectedTokenId, setSelectedTokenId] = useState('');
  const [tokenKey, setTokenKey] = useState('');
  const [loadingTokens, setLoadingTokens] = useState(false);
  const [loadingKey, setLoadingKey] = useState(false);
  const [userModels, setUserModels] = useState([]);
  const [statusState] = useContext(StatusContext);
  const serverAddress = statusState?.status?.server_address || '';

  const baseUrl = serverAddress
    ? serverAddress.replace(/\/$/, '')
    : window.location.origin;

  // 加载令牌和模型
  useEffect(() => {
    setLoadingTokens(true);
    Promise.all([
      API.get('/api/token/?p=1&size=100'),
      API.get('/api/user/models'),
    ])
      .then(([tokenRes, modelRes]) => {
        if (tokenRes.data.success) {
          const items = tokenRes.data.data?.items || [];
          const active = items.filter((tk) => tk.status === 1);
          setTokens(active);
          if (active.length > 0) setSelectedTokenId(String(active[0].id));
        }
        if (modelRes.data.success) {
          setUserModels(modelRes.data.data || []);
        }
      })
      .catch(() => showError(t('加载失败')))
      .finally(() => setLoadingTokens(false));
  }, []);

  // 选中令牌后获取 key
  useEffect(() => {
    if (!selectedTokenId) {
      setTokenKey('');
      return;
    }
    setLoadingKey(true);
    setTokenKey('');
    API.post(`/api/token/${selectedTokenId}/key`)
      .then((res) => {
        if (res.data.success) setTokenKey(res.data.data?.key || '');
        else showError(res.data.message);
      })
      .catch(() => showError(t('获取密钥失败')))
      .finally(() => setLoadingKey(false));
  }, [selectedTokenId]);

  // 生成 models.json
  const modelsJson = useCallback(() => {
    if (!tokenKey || userModels.length === 0) return '';
    const models = userModels.map((name) => {
      const p = getModelParams(name);
      return {
        id: name,
        name: `ERKE ${name}`,
        provider: 'openai',
        url: `${baseUrl}/v1`,
        apiKey: tokenKey,
        maxInputTokens: p.in,
        maxOutputTokens: p.out,
        supportsToolCall: p.tools,
        supportsImages: p.vision,
        supportsReasoning: p.reasoning,
      };
    });
    return JSON.stringify({ models }, null, 2);
  }, [tokenKey, userModels, baseUrl]);

  const handleDownload = useCallback(() => {
    const json = modelsJson();
    if (!json) {
      showError(t('请先选择令牌'));
      return;
    }
    // 无 BOM 的 UTF-8（带 BOM 会导致部分客户端解析失败）
    const blob = new Blob([json], { type: 'application/json;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'models.json';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    showSuccess(t('配置文件已下载'));
  }, [modelsJson, t]);

  // 生成自动替换脚本（type: 'workbuddy' | 'codebuddy'）
  const autoScript = useCallback((type = 'workbuddy') => {
    const json = modelsJson();
    if (!json) return '';
    const dirName = type === 'codebuddy' ? '.codebuddy' : '.workbuddy';
    const productName = type === 'codebuddy' ? 'CodeBuddy' : 'WorkBuddy';

    // 生成 PowerShell 脚本，然后 base64 编码（UTF-16LE），用 bat 调用
    // 这样 bat 可双击执行，PowerShell 处理 JSON 避免特殊字符问题
    const psScript = `$dir = Join-Path $env:USERPROFILE '${dirName}'
if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
$json = @'
${json}
'@
# 使用无 BOM 的 UTF-8 编码写入（带 BOM 会导致客户端解析失败）
$utf8NoBom = New-Object System.Text.UTF8Encoding $false
[System.IO.File]::WriteAllText((Join-Path $dir 'models.json'), $json, $utf8NoBom)
Write-Host ''
Write-Host '✅ 配置已写入: ' (Join-Path $dir 'models.json') -ForegroundColor Green
Write-Host '📊 共 ${userModels.length} 个模型'
Write-Host '🔗 API: ${baseUrl}/v1'
Write-Host ''
Write-Host '重启 ${productName} 即可生效'`;

    // PowerShell -EncodedCommand 要求 UTF-16LE + Base64
    const utf16le = [];
    for (let i = 0; i < psScript.length; i++) {
      const c = psScript.charCodeAt(i);
      utf16le.push(c & 0xff);
      utf16le.push((c >> 8) & 0xff);
    }
    const b64 = btoa(String.fromCharCode(...utf16le));

    return `@echo off
powershell -ExecutionPolicy Bypass -EncodedCommand ${b64}
pause`;
  }, [modelsJson, userModels.length, baseUrl]);

  const handleDownloadScript = useCallback((type = 'workbuddy') => {
    const script = autoScript(type);
    if (!script) {
      showError(t('请先选择令牌'));
      return;
    }
    const blob = new Blob([script], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `setup-${type}.bat`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    showSuccess(t('脚本已下载'));
  }, [autoScript, t]);

  const handleCopyScript = useCallback((type = 'workbuddy') => {
    const script = autoScript(type);
    if (!script) {
      showError(t('请先选择令牌'));
      return;
    }
    navigator.clipboard.writeText(script).then(() => {
      showSuccess(t('脚本已复制到剪贴板'));
    });
  }, [autoScript, t]);

  const tokenOptions = tokens.map((tk) => ({
    value: String(tk.id),
    label: tk.name || `Token #${tk.id}`,
  }));

  const ready = tokenKey && userModels.length > 0;

  return (
    <div className='mt-[60px] px-4 pb-8' style={{ maxWidth: 720, margin: '60px auto 0' }}>
      <Title heading={3} style={{ marginBottom: 4 }}>
        {t('使用教程')}
      </Title>
      <Text type='tertiary'>
        {t('下载配置文件，一键替换 WorkBuddy / CodeBuddy 设置')}
      </Text>

      {/* 第一步：选择令牌 */}
      <Card bordered style={{ marginTop: 24, padding: '20px 24px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 16 }}>
          <div style={{
            width: 28, height: 28, borderRadius: '50%',
            background: ready ? 'var(--semi-color-success)' : 'var(--semi-color-primary)',
            color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: 14, fontWeight: 'bold',
          }}>
            {ready ? <Check size={16} /> : '1'}
          </div>
          <Text strong style={{ fontSize: 16 }}>{t('选择令牌')}</Text>
        </div>

        {loadingTokens ? (
          <div style={{ textAlign: 'center', padding: 16 }}>
            <Spin />
          </div>
        ) : tokens.length === 0 ? (
          <Empty description={t('暂无可用令牌，请先在「令牌」页面创建')} />
        ) : (
          <div>
            <Select
              value={selectedTokenId}
              onChange={setSelectedTokenId}
              style={{ width: '100%' }}
              optionList={tokenOptions}
              placeholder={t('请选择令牌')}
            />
            <div style={{ marginTop: 12 }}>
              {loadingKey ? (
                <Spin size='small' />
              ) : tokenKey ? (
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <Tag size='small' color='green' type='solid'>
                    {t('✓ 密钥已获取')}
                  </Tag>
                  <Text type='tertiary' size='small'>
                    {tokenKey.slice(0, 16)}...{tokenKey.slice(-4)}
                  </Text>
                </div>
              ) : (
                <Text type='tertiary' size='small'>
                  {t('选择令牌后自动获取 API Key')}
                </Text>
              )}
            </div>
          </div>
        )}
      </Card>

      {/* 第二步：一键配置 */}
      <Card bordered style={{ marginTop: 16, padding: '20px 24px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 16 }}>
          <div style={{
            width: 28, height: 28, borderRadius: '50%',
            background: ready ? 'var(--semi-color-success)' : 'var(--semi-color-fill-2)',
            color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: 14, fontWeight: 'bold',
          }}>
            {ready ? <Check size={16} /> : '2'}
          </div>
          <Text strong style={{ fontSize: 16 }}>{t('一键自动配置')}</Text>
        </div>

        {!ready ? (
          <Text type='tertiary'>{t('请先选择令牌')}</Text>
        ) : (
          <div>
            <Paragraph type='tertiary' size='small' style={{ marginBottom: 16 }}>
              {t('选择以下任一方式，自动替换配置文件：')}
            </Paragraph>

            {/* 方式一：复制脚本直接运行 */}
            <div style={{
              border: '1px solid var(--semi-color-border)',
              borderRadius: 8,
              padding: 16,
              marginBottom: 12,
              background: 'var(--semi-color-fill-0)',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                <Terminal size={18} />
                <Text strong>{t('方式一：一键自动配置脚本')}</Text>
              </div>
              <Text type='tertiary' size='small' style={{ display: 'block', marginBottom: 12 }}>
                {t('复制到 CMD 运行，或下载后双击执行：')}
              </Text>

              {/* WorkBuddy 脚本 */}
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                <Tag size='small' color='blue'>WorkBuddy</Tag>
                <Text type='tertiary' size='small'>~/.workbuddy/models.json</Text>
              </div>
              <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
                <Button
                  theme='solid'
                  type='primary'
                  size='small'
                  icon={<Terminal size={14} />}
                  onClick={() => handleCopyScript('workbuddy')}
                >
                  {t('复制脚本')}
                </Button>
                <Button
                  size='small'
                  icon={<Download size={14} />}
                  onClick={() => handleDownloadScript('workbuddy')}
                >
                  {t('下载 .bat')}
                </Button>
              </div>

              {/* CodeBuddy 脚本 */}
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                <Tag size='small' color='green'>CodeBuddy</Tag>
                <Text type='tertiary' size='small'>~/.codebuddy/models.json</Text>
              </div>
              <div style={{ display: 'flex', gap: 8 }}>
                <Button
                  theme='solid'
                  type='primary'
                  size='small'
                  icon={<Terminal size={14} />}
                  onClick={() => handleCopyScript('codebuddy')}
                >
                  {t('复制脚本')}
                </Button>
                <Button
                  size='small'
                  icon={<Download size={14} />}
                  onClick={() => handleDownloadScript('codebuddy')}
                >
                  {t('下载 .bat')}
                </Button>
              </div>
            </div>

            {/* 方式二：下载 models.json 手动替换 */}
            <div style={{
              border: '1px solid var(--semi-color-border)',
              borderRadius: 8,
              padding: 16,
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                <Download size={18} />
                <Text strong>{t('方式二：下载 models.json 手动替换')}</Text>
              </div>
              <Text type='tertiary' size='small' style={{ display: 'block', marginBottom: 8 }}>
                {t('下载后替换到')} <Text code>~/.workbuddy/models.json</Text> {t('或')} <Text code>~/.codebuddy/models.json</Text>
              </Text>
              <Button
                theme='solid'
                type='tertiary'
                icon={<Download size={14} />}
                onClick={handleDownload}
              >
                {t('下载 models.json')}
              </Button>
            </div>
          </div>
        )}
      </Card>

      {/* 配置预览 */}
      {ready && (
        <Card bordered title={t('配置预览')} style={{ marginTop: 16 }}>
          <pre style={{
            background: 'var(--semi-color-fill-0)',
            borderRadius: 8,
            padding: 12,
            maxHeight: 300,
            overflow: 'auto',
            fontSize: 12,
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-all',
          }}>
            {modelsJson()}
          </pre>
        </Card>
      )}

      {/* 可用模型列表 */}
      {userModels.length > 0 && (
        <Card bordered style={{ marginTop: 16 }}>
          <Text strong style={{ display: 'block', marginBottom: 12 }}>
            {t('您的可用模型')}（{userModels.length}）
          </Text>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
            {userModels.map((model) => (
              <Tag key={model} size='small' color='blue'>
                {model}
              </Tag>
            ))}
          </div>
        </Card>
      )}

      <Divider />

      <Banner
        type='info'
        description={t(
          '模型列表根据您的令牌权限动态生成。如需更多模型，请联系管理员调整令牌的模型权限。',
        )}
      />
    </div>
  );
};

export default UsageGuide;
