import React, { useState, useEffect, useCallback, useContext, useMemo } from 'react';
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
  Modal,
} from '@douyinfe/semi-ui';
import { Download, Terminal, Check, Copy, Save, ChevronDown, ChevronUp, MousePointerClick, Keyboard } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, showInfo, showSuccess, timestamp2string } from '../../helpers';
import { StatusContext } from '../../context/Status';
import { UserContext } from '../../context/User';
import StepBadge from './StepBadge';

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
  // savedTemplate: 数据库里已保存的模板（用户配置真正的数据来源）。
  // 编辑器里的 template 只是本地编辑草稿，未点「保存模板」前不影响用户看到的配置。
  const [savedTemplate, setSavedTemplate] = useState('');
  const [templateLoaded, setTemplateLoaded] = useState(false);
  const [savingTemplate, setSavingTemplate] = useState(false);
  // 管理员模板编辑区是否展开（默认收起，保持页面简洁）
  const [tplEditorOpen, setTplEditorOpen] = useState(false);

  // 暂不可用模型的渠道预计恢复时间（model_name -> {recovery_at}）
  const [modelRecoveryMap, setModelRecoveryMap] = useState({});

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
    ])
      .then(([tokenRes, modelRes, metaRes, allModelRes, allMetaRes, templateRes, recoveryRes]) => {
        if (recoveryRes?.data?.success) {
          const rm = {};
          for (const it of recoveryRes.data.data || []) {
            rm[it.model_name] = {
              recovery_at: it.recovery_at || 0,
            };
          }
          setModelRecoveryMap(rm);
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
  const userModelsSet = useMemo(() => new Set(userModels), [userModels]);
  const disabledModels = useMemo(
    () => allUserModels.filter((m) => !userModelsSet.has(m)),
    [allUserModels, userModelsSet],
  );

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
  const modelsJson = useMemo(() => {
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
    const json = modelsJson;
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
    const json = modelsJson;
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

  // 「复制命令」模式：PowerShell 单行命令（irm 配置接口 | iex），
  // 用户复制 → Win+R 粘贴 powershell 回车 → 再粘贴命令回车即完成，无需下载 bat。
  const oneLineCommand = useCallback((type = 'workbuddy') => {
    if (!tokenKey) return '';
    const url = `${baseUrl}/api/usage/guide_config?token_id=${selectedTokenId}&product=${type}`;
    return `irm "${url}" | iex`;
  }, [baseUrl, tokenKey, selectedTokenId]);

  // 引导弹窗打开状态（复制命令后展示傻瓜化三步指引）
  const [guideModalOpen, setGuideModalOpen] = useState(false);
  const [guideModalProduct, setGuideModalProduct] = useState('workbuddy');

  const handleCopyCommand = useCallback((type = 'workbuddy') => {
    const cmd = oneLineCommand(type);
    if (!cmd) {
      showError(t('请先选择令牌'));
      return;
    }
    navigator.clipboard.writeText(cmd).then(() => {
      setGuideModalProduct(type);
      setGuideModalOpen(true);
    });
  }, [oneLineCommand, t]);

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

  const tokenOptions = useMemo(
    () =>
      tokens.map((tk) => ({
        value: String(tk.id),
        label: tk.name || `Token #${tk.id}`,
      })),
    [tokens],
  );

  const ready = tokenKey && effectiveModels.length > 0;

  return (
    <div className='mt-[60px] px-4 pb-8' style={{ maxWidth: 1080, margin: '60px auto 0' }}>
      <Title heading={3} style={{ marginBottom: 4 }}>
        {t('使用教程')}
      </Title>
      <Text type='tertiary'>
        {t('下载配置文件，一键替换 WorkBuddy / CodeBuddy 设置')}
      </Text>

      {/* 顶部醒目：可用 / 暂不可用模型状态总览 */}
      {userModels.length > 0 && (
        <Card bordered style={{ marginTop: 16, padding: '16px 20px', borderColor: disabledModels.length > 0 ? 'var(--semi-color-warning)' : 'var(--semi-color-success)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
            <StepBadge ready size={22} />
            <Text strong>{t('可用模型')}（{userModels.length}）</Text>
          </div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
            {userModels.map((m) => (
              <Tag key={m} size='small' color='green' shape='circle' type='solid'>
                {m}
              </Tag>
            ))}
          </div>

          {disabledModels.length > 0 && (
            <>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 12, marginBottom: 8 }}>
                <StepBadge size={22} idleBackground='var(--semi-color-warning)' content='!' />
                <Text strong>{t('暂不可用模型')}（{disabledModels.length}）</Text>
                <Text type='tertiary' size='small'>{t('已包含在配置中，渠道恢复后立即可用')}</Text>
              </div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                {disabledModels.map((m) => {
                  const rec = modelRecoveryMap[m];
                  const hasRecoveryTime = rec && rec.recovery_at > 0;
                  const tooltip = hasRecoveryTime
                    ? t('预计恢复时间：${time}').replace('${time}', timestamp2string(rec.recovery_at))
                    : t('暂无预计恢复时间');
                  return (
                    <Tooltip key={m} content={tooltip}>
                      <Tag size='small' color='orange' shape='circle'>
                        {m}
                        {hasRecoveryTime && ` · ${t('预计')} ${timestamp2string(rec.recovery_at)}`}
                      </Tag>
                    </Tooltip>
                  );
                })}
              </div>
            </>
          )}
        </Card>
      )}

      {/* 第一步：选择令牌（全宽） */}
      <Card bordered style={{ marginTop: 24, padding: '20px 24px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 16 }}>
          <StepBadge step='1' ready={ready} />
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

      {/* 第二步：一键配置（models.json） */}
      <Card bordered style={{ marginTop: 16, padding: '20px 24px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 16 }}>
          <StepBadge step='2' ready={ready} idleBackground='var(--semi-color-fill-2)' />
          <Text strong style={{ fontSize: 16 }}>{t('配置文件（models.json）')}</Text>
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
            {/* 推荐方式：复制一条命令到 PowerShell 运行 */}
            <Card bordered style={{ borderColor: 'var(--semi-color-success)', background: 'var(--semi-color-success-light-default)', marginBottom: 12 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                <StepBadge ready size={20} />
                <Text strong>{t('推荐：复制命令自动配置')}</Text>
              </div>
              <Text type='tertiary' size='small' style={{ display: 'block', marginBottom: 8 }}>
                {t('① 复制命令 → ② 按 Win+R 输入 powershell 回车 → ③ 粘贴命令回车，自动写入配置')}
              </Text>
              {['workbuddy', 'codebuddy'].map((type) => (
                <div key={type} style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                  <Tag size='small' color='blue'>{type === 'workbuddy' ? 'WorkBuddy' : 'CodeBuddy'}</Tag>
                  <code style={{
                    flex: 1,
                    padding: '4px 10px',
                    background: 'var(--semi-color-fill-1)',
                    borderRadius: 6,
                    fontSize: 11,
                    fontFamily: 'monospace',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}>
                    {oneLineCommand(type)}
                  </code>
                  <Button
                    size='small'
                    type='primary'
                    icon={<Copy size={12} />}
                    onClick={() => handleCopyCommand(type)}
                  >
                    {t('复制命令')}
                  </Button>
                </div>
              ))}
            </Card>

            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
              <Button
                type='tertiary'
                icon={<Download size={14} />}
                onClick={() => handleDownloadScript('workbuddy')}
              >
                {t('WorkBuddy 一键配置(.bat)')}
              </Button>
              <Button
                type='tertiary'
                icon={<Download size={14} />}
                onClick={() => handleDownloadScript('codebuddy')}
              >
                {t('CodeBuddy 一键配置(.bat)')}
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
                {modelsJson}
              </pre>
            </div>

            <Banner
              type='info'
              description={t('推荐使用上方「复制命令」方式；也可下载 .bat 双击运行，或手动下载 models.json 放入对应目录。')}
              style={{ marginTop: 12 }}
            />
          </div>
        )}
      </Card>

      {/* 复制命令后的傻瓜化三步指引弹窗 */}
      <Modal
        visible={guideModalOpen}
        onCancel={() => setGuideModalOpen(false)}
        footer={
          <Button type='primary' onClick={() => setGuideModalOpen(false)}>
            {t('我已完成配置')}
          </Button>
        }
        title={t('命令已复制，按下面 3 步操作')}
        width={520}
        size='medium'
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 14, paddingTop: 4 }}>
          {[
            {
              icon: <Check size={18} />,
              title: t('第 1 步：命令已复制好'),
              desc: guideModalProduct === 'workbuddy'
                ? t('配置 WorkBuddy 的命令已经在你的剪贴板里，不用动它')
                : t('配置 CodeBuddy 的命令已经在你的剪贴板里，不用动它'),
            },
            {
              icon: <Keyboard size={18} />,
              title: t('第 2 步：打开 PowerShell'),
              desc: t('按键盘 Win + R（Win 键在左下角 Ctrl 旁边），弹出的框里输入 powershell，按回车'),
            },
            {
              icon: <MousePointerClick size={18} />,
              title: t('第 3 步：粘贴并回车'),
              desc: t('在打开的蓝色窗口里点鼠标右键（自动粘贴），再按回车，看到 ✅ 提示即完成'),
            },
          ].map((step, idx) => (
            <div key={idx} style={{ display: 'flex', gap: 12, alignItems: 'flex-start' }}>
              <div style={{
                width: 34,
                height: 34,
                borderRadius: '50%',
                background: 'var(--semi-color-primary-light-default)',
                color: 'var(--semi-color-primary)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                flexShrink: 0,
              }}>
                {step.icon}
              </div>
              <div>
                <Text strong style={{ display: 'block', marginBottom: 2 }}>{step.title}</Text>
                <Text type='tertiary' size='small'>{step.desc}</Text>
              </div>
            </div>
          ))}
          <Banner
            type='info'
            closeIcon={null}
            description={t('配置完成后重启 WorkBuddy / CodeBuddy 生效。之后模型有变化，重新复制一次命令运行即可更新。')}
          />
        </div>
      </Modal>


      {/* 管理员：模板编辑（默认收起，移到页面底部不干扰用户视图） */}
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
              <div style={{ display: 'flex', gap: 8, marginBottom: 12, flexWrap: 'wrap' }}>
                <Button
                  size='small'
                  type='tertiary'
                  onClick={handleFillFromAuto}
                  disabled={!ready}
                  icon={<Copy size={12} />}
                >
                  {t('从当前自动生成填充')}
                </Button>
                {savedTemplate.trim() && (
                  <Tag size='small' color='blue'>{t('已启用自定义模板')}</Tag>
                )}
                {!savedTemplate.trim() && (
                  <Tag size='small' color='grey'>{t('当前使用系统自动生成')}</Tag>
                )}
              </div>
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
    </div>
  );
};

export default UsageGuide;
