import React, { useState, useEffect, useCallback } from 'react';
import {
  Modal,
  Select,
  Spin,
  Typography,
  Tabs,
  TabPane,
  Button,
  Empty,
  Tag,
  Card,
} from '@douyinfe/semi-ui';
import { Copy, Check, ExternalLink, Plus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../../helpers';

const { Text, Title, Paragraph } = Typography;

const QuickConfigModal = ({ visible, onClose, t, serverAddress }) => {
  const [tokens, setTokens] = useState([]);
  const [loading, setLoading] = useState(false);
  const [selectedTokenId, setSelectedTokenId] = useState('');
  const [tokenKey, setTokenKey] = useState('');
  const [fetchingKey, setFetchingKey] = useState(false);
  const [models, setModels] = useState([]);
  const [loadingModels, setLoadingModels] = useState(false);
  const [copiedField, setCopiedField] = useState('');

  const baseUrl = serverAddress
    ? serverAddress.replace(/\/$/, '')
    : window.location.origin;

  // 加载令牌列表
  useEffect(() => {
    if (!visible) return;
    setLoading(true);
    API.get('/api/token/?p=1&size=100')
      .then((res) => {
        if (res.data.success) {
          const items = res.data.data?.items || res.data.data || [];
          // 只显示启用的令牌
          const activeTokens = items.filter((t) => t.status === 1);
          setTokens(activeTokens);
          if (activeTokens.length > 0) {
            setSelectedTokenId(String(activeTokens[0].id));
          }
        } else {
          showError(res.data.message);
        }
      })
      .catch(() => showError(t('加载令牌列表失败')))
      .finally(() => setLoading(false));
  }, [visible, t]);

  // 加载可用模型列表
  useEffect(() => {
    if (!visible) return;
    setLoadingModels(true);
    API.get('/api/user/models')
      .then((res) => {
        if (res.data.success) {
          setModels(res.data.data || []);
        }
      })
      .catch(() => {})
      .finally(() => setLoadingModels(false));
  }, [visible]);

  // 选中令牌后获取 key
  useEffect(() => {
    if (!selectedTokenId || !visible) {
      setTokenKey('');
      return;
    }
    setFetchingKey(true);
    setTokenKey('');
    API.post(`/api/token/${selectedTokenId}/key`)
      .then((res) => {
        if (res.data.success) {
          setTokenKey(res.data.data || '');
        } else {
          showError(res.data.message);
        }
      })
      .catch(() => showError(t('获取令牌密钥失败')))
      .finally(() => setFetchingKey(false));
  }, [selectedTokenId, visible, t]);

  const handleCopy = useCallback(
    (text, field) => {
      if (!text) return;
      navigator.clipboard
        .writeText(text)
        .then(() => {
          showSuccess(t('已复制到剪贴板'));
          setCopiedField(field);
          setTimeout(() => setCopiedField(''), 2000);
        })
        .catch(() => showError(t('复制失败')));
    },
    [t],
  );

  // 生成 WorkBuddy JSON 配置
  const workbuddyConfig = React.useMemo(() => {
    if (!tokenKey || models.length === 0) return '';
    const configs = models.map((model) => {
      const name = typeof model === 'string' ? model : model.id || model.name;
      const supportsReasoning =
        /think|reason|o1|o3|o4|glm-5|deepseek-v4|qwen3/i.test(name);
      const supportsToolCall = !/embed|rerank|tts|whisper|image|dall|midjourney|stable/i.test(
        name,
      );
      return {
        id: name,
        name: `${name}`,
        provider: 'openai',
        url: `${baseUrl}/v1`,
        apiKey: tokenKey,
        maxInputTokens: 1000000,
        maxOutputTokens: 64000,
        supportsToolCall,
        supportsImages: /vision|glm-4v|gpt-4o|claude|gemini/i.test(name),
        supportsReasoning,
      };
    });
    return JSON.stringify(configs, null, 2);
  }, [tokenKey, models, baseUrl]);

  // 通用配置（Base URL + API Key）
  const generalConfig = React.useMemo(() => {
    if (!tokenKey) return '';
    return `Base URL: ${baseUrl}/v1\nAPI Key: ${tokenKey}`;
  }, [tokenKey, baseUrl]);

  const tokenOptions = tokens.map((t) => ({
    value: String(t.id),
    label: `${t.name}${t.key ? ` (${t.key.slice(0, 8)}...)` : ''}`,
  }));

  const renderCopyButton = (text, field) => (
    <Button
      size='small'
      icon={copiedField === field ? <Check size={14} /> : <Copy size={14} />}
      onClick={() => handleCopy(text, field)}
      type={copiedField === field ? 'primary' : 'tertiary'}
    >
      {copiedField === field ? t('已复制') : t('复制')}
    </Button>
  );

  return (
    <Modal
      title={t('快捷配置')}
      visible={visible}
      onCancel={onClose}
      footer={null}
      width={680}
      centered
    >
      {/* 第一步：选择令牌 */}
      <div style={{ marginBottom: 16 }}>
        <Text strong style={{ display: 'block', marginBottom: 8 }}>
          {t('1. 选择令牌')}
        </Text>
        {loading ? (
          <div style={{ textAlign: 'center', padding: 20 }}>
            <Spin />
          </div>
        ) : tokens.length === 0 ? (
          <Empty
            description={t('暂无可用令牌，请先创建令牌')}
            style={{ padding: 20 }}
          />
        ) : (
          <Select
            value={selectedTokenId}
            onChange={setSelectedTokenId}
            style={{ width: '100%' }}
            optionList={tokenOptions}
            placeholder={t('请选择令牌')}
          />
        )}
      </div>

      {/* 第二步：展示配置 */}
      {fetchingKey ? (
        <div style={{ textAlign: 'center', padding: 30 }}>
          <Spin tip={t('获取令牌密钥...')} />
        </div>
      ) : tokenKey ? (
        <div>
          <Tabs type='line'>
            {/* WorkBuddy 配置 */}
            <TabPane tab={t('WorkBuddy 配置')} itemKey='workbuddy'>
              <div style={{ marginTop: 12 }}>
                <div
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    marginBottom: 8,
                  }}
                >
                  <Text type='secondary' size='small'>
                    {t('复制以下 JSON 配置到 WorkBuddy CLI')}
                  </Text>
                  {renderCopyButton(workbuddyConfig, 'workbuddy')}
                </div>
                <pre
                  style={{
                    background: 'var(--semi-color-fill-0)',
                    borderRadius: 8,
                    padding: 12,
                    maxHeight: 300,
                    overflow: 'auto',
                    fontSize: 12,
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-all',
                  }}
                >
                  {workbuddyConfig}
                </pre>
              </div>
            </TabPane>

            {/* 通用配置 */}
            <TabPane tab={t('通用配置')} itemKey='general'>
              <div style={{ marginTop: 12 }}>
                <Card
                  bordered
                  style={{ marginBottom: 12 }}
                  bodyStyle={{ padding: '12px 16px' }}
                >
                  <div
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      marginBottom: 4,
                    }}
                  >
                    <Text strong>Base URL</Text>
                    {renderCopyButton(`${baseUrl}/v1`, 'baseurl')}
                  </div>
                  <Text copyable code>
                    {baseUrl}/v1
                  </Text>
                </Card>
                <Card
                  bordered
                  bodyStyle={{ padding: '12px 16px' }}
                >
                  <div
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      marginBottom: 4,
                    }}
                  >
                    <Text strong>API Key</Text>
                    {renderCopyButton(tokenKey, 'apikey')}
                  </div>
                  <Text copyable code>
                    {tokenKey}
                  </Text>
                </Card>
              </div>
            </TabPane>

            {/* 可用模型列表 */}
            <TabPane
              tab={t('可用模型')} itemKey='models'
            >
              <div style={{ marginTop: 12 }}>
                <div
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    marginBottom: 8,
                  }}
                >
                  <Text type='secondary' size='small'>
                    {loadingModels
                      ? t('加载中...')
                      : t('共')} {models.length} {t('个可用模型')}
                  </Text>
                  {renderCopyButton(models.join('\n'), 'models')}
                </div>
                <div
                  style={{
                    maxHeight: 250,
                    overflow: 'auto',
                    display: 'flex',
                    flexWrap: 'wrap',
                    gap: 4,
                  }}
                >
                  {models.map((model) => (
                    <Tag
                      key={model}
                      size='small'
                      color='light'
                      style={{ margin: 0 }}
                    >
                      {model}
                    </Tag>
                  ))}
                </div>
              </div>
            </TabPane>
          </Tabs>
        </div>
      ) : (
        !loading &&
        tokens.length > 0 && (
          <div style={{ textAlign: 'center', padding: 20 }}>
            <Spin tip={t('准备中...')} />
          </div>
        )
      )}
    </Modal>
  );
};

export default QuickConfigModal;
