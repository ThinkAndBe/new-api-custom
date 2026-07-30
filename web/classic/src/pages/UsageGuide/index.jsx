import React, { useState, useEffect, useCallback, useContext } from 'react';
import {
  Card,
  Spin,
  Typography,
  Button,
  Tag,
  Banner,
  Empty,
  Select,
  Tabs,
  TextArea,
  Tooltip,
  Collapsible,
} from '@douyinfe/semi-ui';
import { Download, Terminal, Check, Copy, Save, ChevronDown, ChevronUp } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, showInfo, showSuccess, timestamp2string } from '../../helpers';
import { StatusContext } from '../../context/Status';
import { UserContext } from '../../context/User';

const { Title, Text } = Typography;

// 当后端模型参数未配置时的兜底默认值
const DEFAULT_MODEL_PARAMS = { in: 0, out: 0, tools: false, vision: false, reasoning: false };

// 构建 MCP 服务接入「一键提示词」：把可用工具以自然语言清单形式组织，
// 粘到 zcode / Claude / 任意聊天助手即可让对方知道有哪些 MCP 工具可调用。
// 对不会编辑 config.json 的用户最友好——Zero-touch onboarding。
function buildMcpPrompt(server, serverName, mcpUrl, tokenKey) {
  const toolBrief = (Array.isArray(server.tools) && server.tools.length > 0)
    ? server.tools.map((tool) => {
        const name = tool?.name || 'unknown';
        const desc = (tool?.description || '').trim().split('\n')[0];
        const required = Array.isArray(tool?.inputSchema?.required) && tool.inputSchema.required.length > 0
          ? `（必填：${tool.inputSchema.required.join(', ')}）`
          : '';
        return desc ? `- ${name}：${desc}${required}` : `- ${name}${required}`;
      }).join('\n')
    : '';
  const maskedKey = tokenKey ? (tokenKey.slice(0, 8) + '...') : 'sk-xxx';
  const zcodeConfig = {
    mcp: {
      servers: {
        [serverName]: {
          type: 'http',
          url: mcpUrl,
          headers: { Authorization: `Bearer ${tokenKey || 'sk-xxx'}` },
        },
      },
    },
  };
  return [
    `# MCP 工具接入`,
    ``,
    `服务：${server.name}`,
    server.description ? `说明：${server.description}` : '',
    ``,
    `以下工具可通过 MCP 协议调用（由网关中转，使用令牌「${maskedKey}」鉴权）：`,
    toolBrief || '（暂无可用工具，请等待管理员测试渠道）',
    ``,
    `## 接入方式`,
    ``,
    `### 方式一：在 zcode / Cursor / Claude Desktop 中接入`,
    `把以下配置写入对应文件（zcode: ~/.zcode/cli/config.json；Cursor: ~/.cursor/mcp.json；Claude Desktop: claude_desktop_config.json），重启客户端后即可让助手自动调用：`,
    '```json',
    JSON.stringify(zcodeConfig, null, 2),
    '```',
    ``,
    `### 方式二：在对话里直接告诉助手`,
    `如果你的客户端不能接入 MCP，可把上面这一段工具清单贴给助手，`,
    `让它按需用 HTTP POST 调用 ${mcpUrl}（每次请求带 Authorization: Bearer <你的令牌密钥> 头）。`,
    ``,
    `## 调用流程（streamable-http）`,
    `1. POST ${mcpUrl}，body=`+'`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"my-client","version":"1.0"}}}`，从响应头取 `Mcp-Session-Id`',
    `2. 用同一 session 继续发 tools/list、tools/call 即可`,
  ].filter(Boolean).join('\n');
}

const UsageGuide = () => {
  const { t } = useTranslation();
  const [tokens, setTokens] = useState([]);
  const [selectedTokenId, setSelectedTokenId] = useState('');
  const [tokenKey, setTokenKey] = useState('');
  const [loadingTokens, setLoadingTokens] = useState(false);
  const [loadingKey, setLoadingKey] = useState(false);
  const [userModels, setUserModels] = useState([]);
  // allUserModels: 完整清单（含临时被禁用渠道的模型），用于"稳定模式"
  const [allUserModels, setAllUserModels] = useState([]);
  const [allModelParamsMap, setAllModelParamsMap] = useState({});
  const [modelParamsMap, setModelParamsMap] = useState({});
  const [statusState] = useContext(StatusContext);
  const [userState] = useContext(UserContext);
  const isAdmin = (userState?.user?.role || 0) >= 10;

  // 管理员模板编辑
  const [template, setTemplate] = useState('');
  // savedTemplate: 数据库里已保存的模板（用户配置真正的数据来源）。
  // 编辑器里的 template 只是本地编辑草稿，未点「保存模板」前不影响用户看到的配置。
  const [savedTemplate, setSavedTemplate] = useState('');
  const [templateLoaded, setTemplateLoaded] = useState(false);
  const [savingTemplate, setSavingTemplate] = useState(false);
  // 管理员模板编辑区是否展开（默认收起，保持页面简洁）
  const [tplEditorOpen, setTplEditorOpen] = useState(false);

  // 暂不可用模型的渠道预计恢复时间（model_name -> {recovery_at, channel_name}）
  const [modelRecoveryMap, setModelRecoveryMap] = useState({});

  // MCP 服务（仅展示当前用户分组可用的 MCP 渠道，不暴露渠道密钥）
  const [mcpServers, setMcpServers] = useState([]);
  const [copiedMcpKey, setCopiedMcpKey] = useState('');

  const serverAddress = statusState?.status?.server_address || '';
  const baseUrl = serverAddress
    ? serverAddress.replace(/\/$/, '')
    : window.location.origin;

  // 加载令牌、模型名列表、模型参数元数据、管理员模板
  useEffect(() => {
    setLoadingTokens(true);
    Promise.all([
      API.get('/api/token/?p=1&size=100'),
      API.get('/api/user/models'),
      API.get('/api/user/models/meta'),
      // 完整清单（含临时禁用渠道），用于"稳定模式"
      API.get('/api/user/models?all=1'),
      API.get('/api/user/models/meta?all=1'),
      API.get('/api/user/models/template'),
      // 暂不可用模型的渠道预计恢复时间（失败不阻断页面）
      API.get('/api/user/models/recovery').catch(() => null),
      // 用户分组可用的 MCP 服务（未登录/无权限时忽略失败，不阻断页面）
      API.get('/api/user/mcp_servers').catch(() => null),
    ])
      .then(([tokenRes, modelRes, metaRes, allModelRes, allMetaRes, templateRes, recoveryRes, mcpServersRes]) => {
        if (recoveryRes?.data?.success) {
          const rm = {};
          for (const it of recoveryRes.data.data || []) {
            rm[it.model_name] = {
              recovery_at: it.recovery_at || 0,
              channel_name: it.channel_name || '',
            };
          }
          setModelRecoveryMap(rm);
        }
        if (mcpServersRes?.data?.success) {
          setMcpServers(mcpServersRes.data.data || []);
        }
        if (tokenRes.data.success) {
          const items = tokenRes.data.data?.items || [];
          const active = items.filter((tk) => tk.status === 1);
          setTokens(active);
          if (active.length > 0) setSelectedTokenId(String(active[0].id));
        }
        if (modelRes.data.success) {
          setUserModels(modelRes.data.data || []);
        }
        const buildMap = (metaList) => {
          const m = {};
          for (const it of metaList || []) {
            m[it.model_name] = {
              in: it.max_input_tokens || 0,
              out: it.max_output_tokens || 0,
              tools: !!it.supports_tool_call,
              vision: !!it.supports_images,
              reasoning: !!it.supports_reasoning,
            };
          }
          return m;
        };
        if (metaRes.data.success) {
          setModelParamsMap(buildMap(metaRes.data.data));
        }
        if (allModelRes.data.success) {
          setAllUserModels(allModelRes.data.data || []);
        }
        if (allMetaRes.data.success) {
          setAllModelParamsMap(buildMap(allMetaRes.data.data));
        }
        if (templateRes.data.success && templateRes.data.data) {
          // savedTemplate 始终存数据库原文：配置预览/下载的数据来源，
          // 编辑器里的本地草稿（template）未保存前不影响用户看到的配置
          setSavedTemplate(templateRes.data.data);
          // 编辑器里展示美化后的缩进格式，和配置预览排版一致
          if (String(templateRes.data.data).trim()) {
            try {
              const parsed = JSON.parse(templateRes.data.data);
              setTemplate(JSON.stringify(parsed, null, 2));
            } catch (e) {
              setTemplate(templateRes.data.data);
            }
          }
        }
        setTemplateLoaded(true);
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

  // 模型清单：始终使用完整清单（含暂不可用模型），保证配置文件长期稳定
  const effectiveModels = allUserModels;
  const effectiveParamsMap = allModelParamsMap;
  // 暂不可用模型 = 完整清单 - 当前可用（仅做展示，不参与导出）
  const userModelsSet = new Set(userModels);
  const disabledModels = allUserModels.filter((m) => !userModelsSet.has(m));

  // 自动生成 models.json（当管理员未设置模板时的兜底）
  const autoModelsJson = useCallback(() => {
    if (!tokenKey || effectiveModels.length === 0) return '';
    const models = effectiveModels.map((name) => {
      const p = effectiveParamsMap[name] || DEFAULT_MODEL_PARAMS;
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
  }, [tokenKey, effectiveModels, baseUrl, effectiveParamsMap]);

  // 最终 models.json：优先使用管理员已保存的模板（替换占位符），否则自动生成。
  // 注意：这里读的是 savedTemplate（数据库里已保存的模板），而不是编辑器里的
  // 本地草稿 template —— 管理员点了「从当前自动生成填充」但没保存时，
  // 用户侧看到的配置不能跟着草稿变。
  // 模板模式：管理员模板里的模型列表可能滞后（新模型/渠道禁用未同步），
  // 这里用当前完整清单补齐缺失模型并移除已下线的模型，仅保留模板里的参数。
  const modelsJson = useCallback(() => {
    if (savedTemplate.trim()) {
      const replaced = savedTemplate
        .replace(/\{\{apiKey\}\}/g, tokenKey || 'YOUR_API_KEY')
        .replace(/\{\{baseUrl\}\}/g, baseUrl);
      try {
        const parsed = JSON.parse(replaced);
        if (!Array.isArray(parsed.models)) return replaced;
        // 模型标识兜底：有的对象用 id，有的用 name
        const modelId = (m) => m.id || m.name || '';
        // 模板中已有的模型：保留模板参数
        const kept = parsed.models.filter((m) => effectiveModels.includes(modelId(m)));
        const keptIds = new Set(kept.map((m) => modelId(m)));
        // 模板里没有、但当前清单里有的模型（新上线/新分组）：自动补齐
        const added = effectiveModels
          .filter((name) => !keptIds.has(name))
          .map((name) => {
            const p = effectiveParamsMap[name] || DEFAULT_MODEL_PARAMS;
            return {
              id: name,
              name: `ERKE ${name}`,
              provider: 'openai',
              url: `${baseUrl}/v1`,
              apiKey: tokenKey || 'YOUR_API_KEY',
              maxInputTokens: p.in,
              maxOutputTokens: p.out,
              supportsToolCall: p.tools,
              supportsImages: p.vision,
              supportsReasoning: p.reasoning,
            };
          });
        return JSON.stringify({ ...parsed, models: [...kept, ...added] }, null, 2);
      } catch (e) {
        // 模板不是合法 JSON：原样返回，不做处理
        return replaced;
      }
    }
    return autoModelsJson();
  }, [savedTemplate, tokenKey, baseUrl, autoModelsJson, effectiveModels, effectiveParamsMap]);

  const handleDownload = useCallback(() => {
    const json = modelsJson();
    if (!json) {
      showError(t('请先选择令牌'));
      return;
    }
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

  // 生成自动替换脚本
  const autoScript = useCallback((type = 'workbuddy') => {
    const json = modelsJson();
    if (!json) return '';
    const dirName = type === 'codebuddy' ? '.codebuddy' : '.workbuddy';
    const productName = type === 'codebuddy' ? 'CodeBuddy' : 'WorkBuddy';

    const psScript = `$dir = Join-Path $env:USERPROFILE '${dirName}'
if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
$json = @'
${json}
'@
$utf8NoBom = New-Object System.Text.UTF8Encoding $false
[System.IO.File]::WriteAllText((Join-Path $dir 'models.json'), $json, $utf8NoBom)
Write-Host ''
Write-Host '✅ 配置已写入: ' (Join-Path $dir 'models.json') -ForegroundColor Green
Write-Host '📊 共 ${effectiveModels.length} 个模型'
Write-Host '🔗 API: ${baseUrl}/v1'
Write-Host ''
Write-Host '重启 ${productName} 即可生效'`;

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
  }, [modelsJson, effectiveModels.length, baseUrl]);

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

  // MCP 配置复制：按客户端生成一键粘贴的 JSON 配置（url 固定为 <baseUrl>/mcp）
  const copyMcpConfig = (server, client) => {
    if (!tokenKey) {
      showError(t('请先选择令牌'));
      return;
    }
    // 「一键提示词」tab：复制自然语言接入说明（不依赖客户端 config 编辑能力）
    if (client === 'prompt') {
      const text = buildMcpPrompt(server, serverName, mcpUrl, tokenKey);
      navigator.clipboard
        .writeText(text)
        .then(() => {
          setCopiedMcpKey(`${server.id}-prompt`);
          showSuccess(t('已复制 MCP 提示词'));
          setTimeout(() => setCopiedMcpKey(''), 2000);
        })
        .catch(() => showError(t('复制失败')));
      return;
    }
    const serverName = (server.name || `mcp-${server.id}`)
      .replace(/[^\w-]+/g, '-')
      .toLowerCase();
    const url = `${baseUrl.replace(/\/+$/, '')}/mcp`;
    let config;
    if (client === 'zcode') {
      // zcode: ~/.zcode/cli/config.json
      config = {
        mcp: {
          servers: {
            [serverName]: {
              type: 'http',
              url,
              headers: { Authorization: `Bearer ${tokenKey}` },
            },
          },
        },
      };
    } else {
      // Cursor (~/.cursor/mcp.json) / Claude Desktop (claude_desktop_config.json) 均为 mcpServers 结构
      config = {
        mcpServers: {
          [serverName]: {
            type: 'http',
            url,
            headers: { Authorization: `Bearer ${tokenKey}` },
          },
        },
      };
    }
    const text = JSON.stringify(config, null, 2);
    navigator.clipboard
      .writeText(text)
      .then(() => {
        setCopiedMcpKey(`${server.id}-${client}`);
        showSuccess(t('已复制 MCP 配置'));
        setTimeout(() => setCopiedMcpKey(''), 2000);
      })
      .catch(() => showError(t('复制失败')));
  };

  // 管理员保存模板
  const handleSaveTemplate = useCallback(async () => {
    // 验证 JSON 格式
    if (template.trim()) {
      try {
        JSON.parse(template.replace(/\{\{apiKey\}\}/g, 'test').replace(/\{\{baseUrl\}\}/g, 'http://test'));
      } catch (e) {
        showError(t('JSON 格式错误，请检查后重试'));
        return;
      }
    }
    setSavingTemplate(true);
    try {
      const res = await API.put('/api/option/', {
        key: 'ModelsJsonTemplate',
        value: template,
      });
      if (res.data.success) {
        // 保存成功后才更新 savedTemplate，用户侧配置此时才真正变更
        setSavedTemplate(template);
        showSuccess(t('模板已保存，所有用户的使用教程将同步更新'));
      } else {
        showError(res.data.message || t('保存失败'));
      }
    } catch (e) {
      showError(e?.response?.data?.message || t('保存失败'));
    } finally {
      setSavingTemplate(false);
    }
  }, [template, t]);

  // 管理员用当前自动生成的内容填充模板编辑器
  const handleFillFromAuto = useCallback(() => {
    const auto = autoModelsJson();
    if (auto) {
      // 把 apiKey 和 baseUrl 替换为占位符
      const tpl = auto
        .replace(new RegExp(tokenKey.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'g'), '{{apiKey}}')
        .replace(new RegExp(baseUrl.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'g'), '{{baseUrl}}');
      // 只填到编辑器草稿里，不点「保存模板」不会影响用户看到的配置
      setTemplate(tpl);
      showInfo(t('已填充到编辑器，点击「保存模板」后才会生效到用户配置'));
    }
  }, [autoModelsJson, tokenKey, baseUrl, t]);

  const tokenOptions = tokens.map((tk) => ({
    value: String(tk.id),
    label: tk.name || `Token #${tk.id}`,
  }));

  const ready = tokenKey && effectiveModels.length > 0;

  return (
    <div className='mt-[60px] px-4 pb-8' style={{ maxWidth: 720, margin: '60px auto 0' }}>
      <Title heading={3} style={{ marginBottom: 4 }}>
        {t('使用教程')}
      </Title>
      <Text type='tertiary'>
        {t('下载配置文件，一键替换 WorkBuddy / CodeBuddy 设置')}
      </Text>

      {/* 管理员：模板编辑（默认收起，保持页面简洁） */}
      {isAdmin && templateLoaded && (
        <Card bordered style={{ marginTop: 24, padding: '20px 24px', borderColor: 'var(--semi-color-warning)' }}>
          <div
            style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', cursor: 'pointer' }}
            onClick={() => setTplEditorOpen((v) => !v)}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <Text strong style={{ fontSize: 16 }}>{t('管理员：models.json 模板')}</Text>
              <Tag size='small' color='orange'>{t('仅管理员可见')}</Tag>
            </div>
            <Button
              theme='borderless'
              size='small'
              icon={tplEditorOpen ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
            >
              {tplEditorOpen ? t('收起') : t('展开编辑')}
            </Button>
          </div>
          <Collapsible isOpen={tplEditorOpen} keepDOM>
            <div onClick={(e) => e.stopPropagation()} style={{ marginTop: 12 }}>
              <Text type='tertiary' size='small' style={{ display: 'block', marginBottom: 12 }}>
                {t('编辑此模板后保存，所有用户的使用教程将使用此模板生成配置。使用 {{apiKey}} 和 {{baseUrl}} 作为占位符，用户访问时自动替换为其实际值。留空则使用系统自动生成。')}
              </Text>
              <TextArea
                value={template}
                onChange={setTemplate}
                placeholder={t('留空 = 使用系统自动生成。或粘贴完整 models.json 模板，用 {{apiKey}} 和 {{baseUrl}} 作为占位符。')}
                autosize={{ minRows: 10, maxRows: 30 }}
                style={{ fontFamily: 'monospace', fontSize: 12 }}
              />
              <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
                <Button
                  type='primary'
                  icon={<Save size={14} />}
                  loading={savingTemplate}
                  onClick={handleSaveTemplate}
                >
                  {t('保存模板')}
                </Button>
                <Button
                  type='tertiary'
                  onClick={handleFillFromAuto}
                  disabled={!ready}
                >
                  {t('从当前自动生成填充')}
                </Button>
                {template && (
                  <Button
                    type='danger'
                    theme='borderless'
                    onClick={() => setTemplate('')}
                  >
                    {t('清空模板')}
                  </Button>
                )}
              </div>
            </div>
          </Collapsible>
        </Card>
      )}

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
            {savedTemplate.trim() && (
              <Banner
                type='info'
                description={t('当前使用管理员自定义模板')}
                style={{ marginBottom: 12 }}
              />
            )}
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
              <Button
                type='primary'
                icon={<Download size={14} />}
                onClick={() => handleDownloadScript('workbuddy')}
              >
                {t('WorkBuddy 一键配置')}
              </Button>
              <Button
                type='primary'
                icon={<Download size={14} />}
                onClick={() => handleDownloadScript('codebuddy')}
              >
                {t('CodeBuddy 一键配置')}
              </Button>
              <Button
                type='tertiary'
                icon={<Terminal size={14} />}
                onClick={() => handleCopyScript('workbuddy')}
              >
                {t('复制脚本')}
              </Button>
              <Button
                type='tertiary'
                icon={<Download size={14} />}
                onClick={handleDownload}
              >
                {t('下载 models.json')}
              </Button>
            </div>

            {/* 配置预览 */}
            <div style={{ marginTop: 16 }}>
              <Text type='tertiary' size='small'>{t('配置预览')}：</Text>
              <pre style={{
                marginTop: 8,
                padding: 12,
                background: 'var(--semi-color-fill-0)',
                borderRadius: 8,
                fontSize: 11,
                lineHeight: 1.5,
                overflow: 'auto',
                maxHeight: 300,
                // 保持 JSON 原始缩进，长行横向滚动而不是中间断词，避免预览没法看
                whiteSpace: 'pre',
                wordBreak: 'normal',
              }}>
                {modelsJson()}
              </pre>
            </div>

            {/* 可用模型列表（始终只显示当前 enabled=true 的） */}
            <div style={{ marginTop: 12 }}>
              <Text type='tertiary' size='small'>
                {t('可用模型')}（{userModels.length}）：
              </Text>
              <div style={{ marginTop: 6, display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                {userModels.map((m) => (
                  <Tag key={m} size='small' color='blue' shape='circle'>
                    {m}
                  </Tag>
                ))}
              </div>
            </div>

            {/* 暂不可用模型（提示用户这些模型已在配置里，渠道恢复后即可用） */}
            {disabledModels.length > 0 && (
              <div style={{ marginTop: 12 }}>
                <Text type='tertiary' size='small'>
                  {t('暂不可用模型')}（{disabledModels.length}）：
                </Text>
                <div style={{ marginTop: 6, display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                  {disabledModels.map((m) => {
                    const rec = modelRecoveryMap[m];
                    const hasRecoveryTime = rec && rec.recovery_at > 0;
                    const tooltip = hasRecoveryTime
                      ? t('预计恢复时间：${time}').replace('${time}', timestamp2string(rec.recovery_at))
                      : t('暂无预计恢复时间');
                    return (
                      <Tooltip key={m} content={tooltip}>
                        <Tag size='small' color='grey' shape='circle' type='ghost'>
                          {m}
                          {hasRecoveryTime && ` (${t('预计')} ${timestamp2string(rec.recovery_at)})`}
                        </Tag>
                      </Tooltip>
                    );
                  })}
                </div>
                <Text type='tertiary' size='small' style={{ display: 'block', marginTop: 6 }}>
                  {t('这些模型已包含在配置中，渠道恢复后立即可用，无需重新下载。')}
                </Text>
              </div>
            )}
          </div>
        )}
      </Card>

      {/* MCP 服务（当前用户分组可用的 MCP 渠道，一键复制客户端配置） */}
      {mcpServers.length > 0 && (
        <Card bordered style={{ marginTop: 16, padding: '20px 24px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
            <div style={{
              width: 28, height: 28, borderRadius: '50%',
              background: 'var(--semi-color-cyan)',
              color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center',
              fontSize: 14, fontWeight: 'bold',
            }}>
              M
            </div>
            <Text strong style={{ fontSize: 16 }}>{t('MCP 服务')}</Text>
            <Tag color='cyan' shape='circle' size='small'>
              {mcpServers.length}
            </Tag>
          </div>
          <Text type='tertiary' size='small'>
            {t('选择客户端，一键复制配置粘贴到对应配置文件即可使用。所有 MCP 服务共用上方选中的令牌密钥。')}
          </Text>

          {mcpServers.map((server) => {
            const serverName = (server.name || `mcp-${server.id}`)
              .replace(/[^\w-]+/g, '-')
              .toLowerCase();
            const mcpUrl = `${baseUrl.replace(/\/+$/, '')}/mcp`;
            const promptText = buildMcpPrompt(server, serverName, mcpUrl, tokenKey);
            const clients = [
              {
                key: 'prompt',
                name: t('一键提示词'),
                path: '',
                promptText,
              },
              {
                key: 'zcode',
                name: 'ZCode',
                path: '~/.zcode/cli/config.json',
                config: {
                  mcp: {
                    servers: {
                      [serverName]: {
                        type: 'http',
                        url: mcpUrl,
                        headers: {
                          Authorization: `Bearer ${tokenKey || 'sk-xxx'}`,
                        },
                      },
                    },
                  },
                },
              },
              {
                key: 'cursor',
                name: 'Cursor',
                path: '~/.cursor/mcp.json',
                config: {
                  mcpServers: {
                    [serverName]: {
                      type: 'http',
                      url: mcpUrl,
                      headers: {
                        Authorization: `Bearer ${tokenKey || 'sk-xxx'}`,
                      },
                    },
                  },
                },
              },
              {
                key: 'claude',
                name: 'Claude Desktop',
                path: 'claude_desktop_config.json',
                config: {
                  mcpServers: {
                    [serverName]: {
                      type: 'http',
                      url: mcpUrl,
                      headers: {
                        Authorization: `Bearer ${tokenKey || 'sk-xxx'}`,
                      },
                    },
                  },
                },
              },
            ];
            return (
              <Card
                key={server.id}
                bordered
                shadows='hover'
                style={{ marginTop: 16 }}
                bodyStyle={{ padding: '12px 16px' }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                  <Text strong style={{ fontSize: 15 }}>{server.name}</Text>
                  {Array.isArray(server.tools) && server.tools.length > 0 ? (
                    server.tools.map((tool, idx) => (
                      <Tag key={idx} size='small' color='cyan' shape='circle'>
                        {tool?.name || `tool_${idx}`}
                      </Tag>
                    ))
                  ) : (
                    <Tag size='small' color='grey' shape='circle' type='ghost'>
                      {t('未测试')}
                    </Tag>
                  )}
                </div>
                {server.description && (
                  <Text type='tertiary' size='small' style={{ display: 'block', marginTop: 6 }}>
                    {server.description}
                  </Text>
                )}
                <Tabs type='card' style={{ marginTop: 12 }} defaultActiveKey='prompt'>
                  {clients.map((client) => {
                    const copied = copiedMcpKey === `${server.id}-${client.key}`;
                    const isPrompt = client.key === 'prompt';
                    return (
                      <Tabs.TabPane key={client.key} tab={client.name} itemKey={client.key}>
                        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
                          <Text type='tertiary' size='small'>
                            {isPrompt
                              ? t('粘贴到任意 AI 助手对话即可让其调用 MCP 工具，无需编辑配置文件')
                              : <>{t('配置文件路径')}：<Text code size='small'>{client.path}</Text></>
                            }
                          </Text>
                          <Button
                            size='small'
                            type={copied ? 'primary' : 'tertiary'}
                            theme={copied ? 'solid' : 'borderless'}
                            icon={copied ? <Check size={12} /> : <Copy size={12} />}
                            onClick={() => copyMcpConfig(server, client.key)}
                          >
                            {copied ? t('已复制') : (isPrompt ? t('复制提示词') : t('复制配置'))}
                          </Button>
                        </div>
                        <pre style={{
                          marginTop: 8,
                          padding: 12,
                          background: 'var(--semi-color-fill-0)',
                          borderRadius: 8,
                          fontSize: 11,
                          lineHeight: 1.5,
                          overflow: 'auto',
                          maxHeight: isPrompt ? 360 : 220,
                          whiteSpace: 'pre',
                          wordBreak: 'normal',
                        }}>
                          {isPrompt ? (client.promptText || '') : JSON.stringify(client.config, null, 2)}
                        </pre>
                      </Tabs.TabPane>
                    );
                  })}
                </Tabs>
              </Card>
            );
          })}

          <Banner
            type='info'
            description={t('接入地址统一为 <baseUrl>/mcp，客户端通过 Authorization: Bearer <你的令牌密钥> 鉴权；多个 MCP 服务时系统自动从你有权限的分组中选择可用服务。')}
            style={{ marginTop: 16 }}
          />
        </Card>
      )}

      <Banner
        type='info'
        description={t('下载 .bat 脚本后双击运行即可自动配置。也可手动下载 models.json 放入对应目录。')}
        style={{ marginTop: 16 }}
      />
    </div>
  );
};

export default UsageGuide;
