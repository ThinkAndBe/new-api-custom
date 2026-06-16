import React, { useMemo } from 'react';
import { Table, Button, Spin, Empty } from '@douyinfe/semi-ui';
import { IconChevronDown, IconChevronUp, IconRefresh } from '@douyinfe/semi-icons';
import { renderNumber, renderQuota } from '../../../helpers';

const TokenSummaryPanel = ({
  showTokenSummary,
  toggleTokenSummary,
  tokenSummary,
  tokenSummaryLoading,
  loadTokenSummary,
  t,
}) => {
  // 计算总计行
  const totals = useMemo(() => {
    if (!tokenSummary || tokenSummary.length === 0) {
      return { count: 0, quota: 0, prompt: 0, completion: 0, total: 0 };
    }
    return tokenSummary.reduce(
      (acc, row) => ({
        count: acc.count + (row.count || 0),
        quota: acc.quota + (row.quota || 0),
        prompt: acc.prompt + (row.prompt_tokens || 0),
        completion: acc.completion + (row.completion_tokens || 0),
        total: acc.total + (row.total_tokens || 0),
      }),
      { count: 0, quota: 0, prompt: 0, completion: 0, total: 0 },
    );
  }, [tokenSummary]);

  const columns = [
    {
      title: t('令牌名称'),
      dataIndex: 'token_name',
      key: 'token_name',
      width: 240,
      render: (text) => (
        <span style={{ fontFamily: 'monospace', fontSize: 13 }}>{text}</span>
      ),
    },
    {
      title: t('调用次数'),
      dataIndex: 'count',
      key: 'count',
      sorter: (a, b) => a.count - b.count,
      width: 120,
      render: (val) => renderNumber(val),
    },
    {
      title: t('Prompt Tokens'),
      dataIndex: 'prompt_tokens',
      key: 'prompt_tokens',
      sorter: (a, b) => a.prompt_tokens - b.prompt_tokens,
      width: 150,
      render: (val) => renderNumber(val),
    },
    {
      title: t('Completion Tokens'),
      dataIndex: 'completion_tokens',
      key: 'completion_tokens',
      sorter: (a, b) => a.completion_tokens - b.completion_tokens,
      width: 170,
      render: (val) => renderNumber(val),
    },
    {
      title: t('总 Tokens'),
      dataIndex: 'total_tokens',
      key: 'total_tokens',
      sorter: (a, b) => a.total_tokens - b.total_tokens,
      defaultSortOrder: 'descend',
      width: 140,
      render: (val) => <strong>{renderNumber(val)}</strong>,
    },
    {
      title: t('消耗'),
      dataIndex: 'quota',
      key: 'quota',
      sorter: (a, b) => a.quota - b.quota,
      width: 140,
      render: (val) => (
        <strong style={{ color: 'var(--semi-color-primary)' }}>
          {renderQuota(val, 4)}
        </strong>
      ),
    },
  ];

  return (
    <div
      style={{
        marginTop: 8,
        borderRadius: 8,
        border: '1px solid var(--semi-color-border)',
        overflow: 'hidden',
      }}
    >
      {/* 头部：标题 + 切换/刷新按钮 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '10px 16px',
          background: 'var(--semi-color-fill-0)',
          cursor: 'pointer',
        }}
        onClick={toggleTokenSummary}
      >
        <span style={{ fontWeight: 600, fontSize: 14 }}>
          {t('令牌汇总统计')}
          {tokenSummary.length > 0 && (
            <span
              style={{
                marginLeft: 8,
                fontSize: 12,
                color: 'var(--semi-color-tertiary)',
                fontWeight: 400,
              }}
            >
              ({t('共')} {tokenSummary.length} {t('个令牌')})
            </span>
          )}
        </span>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          {showTokenSummary && (
            <Button
              size='small'
              type='tertiary'
              icon={<IconRefresh />}
              onClick={(e) => {
                e.stopPropagation();
                loadTokenSummary();
              }}
            >
              {t('刷新')}
            </Button>
          )}
          {showTokenSummary ? <IconChevronUp /> : <IconChevronDown />}
        </div>
      </div>

      {/* 展开内容 */}
      {showTokenSummary && (
        <div style={{ padding: '12px 16px' }}>
          {tokenSummaryLoading ? (
            <div style={{ textAlign: 'center', padding: 40 }}>
              <Spin size='large' />
            </div>
          ) : tokenSummary.length === 0 ? (
            <Empty
              title={t('暂无数据')}
              description={t('当前筛选条件下无令牌汇总数据')}
            />
          ) : (
            <>
              <Table
                columns={columns}
                dataSource={tokenSummary}
                rowKey='token_name'
                pagination={
                  tokenSummary.length > 10
                    ? { pageSize: 10, showSizeChanger: false }
                    : false
                }
                size='small'
              />
              {/* 汇总统计条 */}
              <div
                style={{
                  display: 'flex',
                  gap: 24,
                  flexWrap: 'wrap',
                  marginTop: 12,
                  padding: '10px 16px',
                  background: 'var(--semi-color-fill-1)',
                  borderRadius: 6,
                  fontSize: 13,
                }}
              >
                <span>
                  <strong>{t('合计')}：</strong>
                </span>
                <span>
                  {t('调用')}: <strong>{renderNumber(totals.count)}</strong>
                </span>
                <span>
                  {t('Prompt')}: <strong>{renderNumber(totals.prompt)}</strong>
                </span>
                <span>
                  {t('Completion')}:{' '}
                  <strong>{renderNumber(totals.completion)}</strong>
                </span>
                <span>
                  {t('总 Tokens')}: <strong>{renderNumber(totals.total)}</strong>
                </span>
                <span>
                  {t('总消耗')}:{' '}
                  <strong style={{ color: 'var(--semi-color-primary)' }}>
                    {renderQuota(totals.quota, 4)}
                  </strong>
                </span>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
};

export default TokenSummaryPanel;
