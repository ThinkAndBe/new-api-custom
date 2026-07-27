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
  TextArea,
} from '@douyinfe/semi-ui';
import { Download, Terminal, Check, Save } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';
import { StatusContext } from '../../context/Status';
import { UserContext } from '../../context/User';

const { Title, Text } = Typography;

// 当后端模型参数未配置时的兜底默认值
const DEFAULT_MODEL_PARAMS = { in: 0, out: 0, tools: false, vision: false, reasoning: false };

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
  const [templateLoaded, setTemplateLoaded] = useState(false);
  const [savingTemplate, setSavingTemplate] = useState(false);

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
    ])
      .then(([tokenRes, modelRes, metaRes, allModelRes, allMetaRes, templateRes]) => {
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
          setTemplate(templateRes.data.data);
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

  // 最终 models.json：优先使用管理员模板（替换占位符），否则自动生成
  // 模板模式：管理员模板里的模型列表可能滞后（新模型/渠道禁用未同步），
  // 这里用当前完整清单补齐缺失模型并移除已下线的模型，仅保留模板里的参数。
  const modelsJson = useCallback(() => {
    if (template.trim()) {
      const replaced = template
        .replace(/\{\{apiKey\}\}/g, tokenKey || 'YOUR_API_KEY')
        .replace(/\{\{baseUrl\}\}/g, baseUrl);
      try {
        const parsed = JSON.parse(replaced);
        if (!Array.isArray(parsed.models)) return replaced;
        // 模板中已有的模型：保留模板参数
        const kept = parsed.models.filter((m) => effectiveModels.includes(m.id));
        const keptIds = new Set(kept.map((m) => m.id));
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
  }, [template, tokenKey, baseUrl, autoModelsJson, effectiveModels, effectiveParamsMap]);

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
      setTemplate(tpl);
    }
  }, [autoModelsJson, tokenKey, baseUrl]);

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

      {/* 管理员：模板编辑 */}
      {isAdmin && templateLoaded && (
        <Card bordered style={{ marginTop: 24, padding: '20px 24px', borderColor: 'var(--semi-color-warning)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12 }}>
            <Text strong style={{ fontSize: 16 }}>{t('管理员：models.json 模板')}</Text>
            <Tag size='small' color='orange'>{t('仅管理员可见')}</Tag>
          </div>
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
            {template.trim() && (
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
                  {disabledModels.map((m) => (
                    <Tag key={m} size='small' color='grey' shape='circle' type='ghost'>
                      {m}
                    </Tag>
                  ))}
                </div>
                <Text type='tertiary' size='small' style={{ display: 'block', marginTop: 6 }}>
                  {t('这些模型已包含在配置中，渠道恢复后立即可用，无需重新下载。')}
                </Text>
              </div>
            )}
          </div>
        )}
      </Card>

      <Banner
        type='info'
        description={t('下载 .bat 脚本后双击运行即可自动配置。也可手动下载 models.json 放入对应目录。')}
        style={{ marginTop: 16 }}
      />
    </div>
  );
};

export default UsageGuide;
