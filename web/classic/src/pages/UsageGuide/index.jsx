import React, { useState, useEffect, useCallback } from 'react';
import {
  Card,
  Select,
  Spin,
  Typography,
  Button,
  Tag,
  Steps,
  Banner,
  Empty,
} from '@douyinfe/semi-ui';
import { Download, Copy, Check, Key } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';
import { UserContext } from '../../context/User';
import { StatusContext } from '../../context/Status';
import { useContext } from 'react';

const { Title, Text, Paragraph, Code } = Typography;

const UsageGuide = () => {
  const { t } = useTranslation();
  const [tokens, setTokens] = useState([]);
  const [selectedTokenId, setSelectedTokenId] = useState('');
  const [tokenKey, setTokenKey] = useState('');
  const [loadingTokens, setLoadingTokens] = useState(false);
  const [loadingKey, setLoadingKey] = useState(false);
  const [userModels, setUserModels] = useState([]);
  const [copied, setCopied] = useState('');
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
  }, [t]);

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
  }, [selectedTokenId, t]);

  const handleCopy = useCallback(
    (text, field) => {
      if (!text) return;
      navigator.clipboard.writeText(text).then(() => {
        showSuccess(t('已复制'));
        setCopied(field);
        setTimeout(() => setCopied(''), 2000);
      });
    },
    [t],
  );

  // 生成 models.json
  const modelsJson = useCallback(() => {
    if (!tokenKey || userModels.length === 0) return '';
    const models = userModels.map((name) => {
      const supportsReasoning =
        /think|reason|o1|o3|o4|glm-5|deepseek-v4|qwen3/i.test(name);
      const supportsToolCall = !/embed|rerank|tts|whisper|dall|midjourney|stable/i.test(
        name,
      );
      const supportsImages = /vision|glm-4v|gpt-4o|claude|gemini|qwen-vl/i.test(
        name,
      );
      return {
        id: name,
        name: name,
        provider: 'openai',
        url: `${baseUrl}/v1`,
        apiKey: tokenKey,
        maxInputTokens: 1000000,
        maxOutputTokens: 64000,
        supportsToolCall,
        supportsImages,
        supportsReasoning,
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
    const blob = new Blob([json], { type: 'application/json' });
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

  const jsonContent = modelsJson();
  const tokenOptions = tokens.map((tk) => ({
    value: String(tk.id),
    label: tk.name || `Token #${tk.id}`,
  }));

  const CopyButton = ({ text, field }) => (
    <Button
      size='small'
      icon={copied === field ? <Check size={14} /> : <Copy size={14} />}
      onClick={() => handleCopy(text, field)}
      type={copied === field ? 'primary' : 'tertiary'}
    >
      {copied === field ? t('已复制') : t('复制')}
    </Button>
  );

  return (
    <div className='mt-[60px] px-4 max-w-4xl mx-auto pb-8'>
      <Title heading={3} style={{ marginBottom: 4 }}>
        {t('使用教程')}
      </Title>
      <Text type='tertiary'>
        {t('配置 WorkBuddy / CodeBuddy 客户端连接到本平台')}
      </Text>

      <Steps
        direction='vertical'
        style={{ marginTop: 24 }}
        current={tokenKey ? 3 : selectedTokenId ? 1 : 0}
      >
        {/* Step 1: 选择令牌 */}
        <Steps.Step
          title={t('1. 选择令牌')}
          description={
            <Card bordered style={{ marginTop: 8, marginBottom: 8 }}>
              {loadingTokens ? (
                <div style={{ textAlign: 'center', padding: 16 }}>
                  <Spin />
                </div>
              ) : tokens.length === 0 ? (
                <Empty
                  description={t('暂无可用令牌，请先在「令牌」页面创建')}
                  style={{ padding: 16 }}
                />
              ) : (
                <div>
                  <Select
                    value={selectedTokenId}
                    onChange={setSelectedTokenId}
                    style={{ width: '100%' }}
                    optionList={tokenOptions}
                    placeholder={t('请选择令牌')}
                  />
                  <div style={{ marginTop: 8 }}>
                    {loadingKey ? (
                      <Spin size='small' />
                    ) : tokenKey ? (
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <Key size={14} />
                        <Text code copyable>
                          {tokenKey.slice(0, 12)}...{tokenKey.slice(-4)}
                        </Text>
                        <Tag size='small' color='green'>
                          {t('已获取密钥')}
                        </Tag>
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
          }
        />

        {/* Step 2: 下载配置 */}
        <Steps.Step
          title={t('2. 下载 models.json 配置文件')}
          description={
            <Card bordered style={{ marginTop: 8, marginBottom: 8 }}>
              {tokenKey ? (
                <div>
                  <div
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      marginBottom: 8,
                    }}
                  >
                    <Text type='secondary' size='small'>
                      {t('根据您的模型权限生成')}（{userModels.length}{' '}
                      {t('个模型')}）
                    </Text>
                    <Button
                      type='primary'
                      theme='solid'
                      icon={<Download size={16} />}
                      onClick={handleDownload}
                    >
                      {t('下载 models.json')}
                    </Button>
                  </div>
                  <pre
                    style={{
                      background: 'var(--semi-color-fill-0)',
                      borderRadius: 8,
                      padding: 12,
                      maxHeight: 200,
                      overflow: 'auto',
                      fontSize: 12,
                      whiteSpace: 'pre-wrap',
                      wordBreak: 'break-all',
                    }}
                  >
                    {jsonContent || t('请先选择令牌')}
                  </pre>
                </div>
              ) : (
                <Text type='tertiary'>{t('请先完成上一步')}</Text>
              )}
            </Card>
          }
        />

        {/* Step 3: 替换配置文件 */}
        <Steps.Step
          title={t('3. 替换 WorkBuddy 配置文件')}
          description={
            <Card bordered style={{ marginTop: 8, marginBottom: 8 }}>
              <Paragraph>
                <Text strong>{t('方法一：直接替换文件')}</Text>
              </Paragraph>
              <Paragraph type='tertiary' size='small'>
                {t('将下载的')} <Code>models.json</Code>{' '}
                {t('文件替换到以下路径：')}
              </Paragraph>
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  marginBottom: 12,
                }}
              >
                <Code copyable>~/.workbuddy/models.json</Code>
              </div>

              <Paragraph style={{ marginTop: 12 }}>
                <Text strong>{t('方法二：手动编辑配置')}</Text>
              </Paragraph>
              <Paragraph type='tertiary' size='small'>
                {t('如果已有配置文件，可手动修改以下字段：')}
              </Paragraph>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <Text size='small' style={{ width: 80 }}>
                    url:
                  </Text>
                  <Code copyable>{baseUrl}/v1</Code>
                  <CopyButton text={`${baseUrl}/v1`} field='url' />
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <Text size='small' style={{ width: 80 }}>
                    apiKey:
                  </Text>
                  <Code copyable>
                    {tokenKey ? `${tokenKey.slice(0, 12)}...` : 'sk-xxx'}
                  </Code>
                  {tokenKey && (
                    <CopyButton text={tokenKey} field='apikey' />
                  )}
                </div>
              </div>
            </Card>
          }
        />

        {/* Step 4: CodeBuddy 配置 */}
        <Steps.Step
          title={t('4. CodeBuddy 配置（可选）')}
          description={
            <Card bordered style={{ marginTop: 8, marginBottom: 8 }}>
              <Paragraph type='tertiary' size='small'>
                {t('CodeBuddy 使用相同配置，只需修改 API 地址和密钥：')}
              </Paragraph>
              <pre
                style={{
                  background: 'var(--semi-color-fill-0)',
                  borderRadius: 8,
                  padding: 12,
                  fontSize: 12,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-all',
                }}
              >{`# CodeBuddy 配置
API Base URL: ${baseUrl}/v1
API Key: ${tokenKey || 'sk-xxx'}`}</pre>
              <div style={{ marginTop: 8 }}>
                <CopyButton
                  text={`API Base URL: ${baseUrl}/v1\nAPI Key: ${tokenKey || 'sk-xxx'}`}
                  field='codebuddy'
                />
              </div>
            </Card>
          }
        />
      </Steps>

      {/* 可用模型列表 */}
      {userModels.length > 0 && (
        <Card
          bordered
          title={t('您的可用模型')}
          style={{ marginTop: 16 }}
        >
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
            {userModels.map((model) => (
              <Tag key={model} size='small' color='blue'>
                {model}
              </Tag>
            ))}
          </div>
        </Card>
      )}

      {/* 提示 */}
      <Banner
        type='info'
        description={t(
          '配置文件中的模型列表根据您的令牌权限动态生成。如需更多模型，请联系管理员调整令牌的模型权限。',
        )}
        style={{ marginTop: 16 }}
      />
    </div>
  );
};

export default UsageGuide;
