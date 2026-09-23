import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Table, Button, Spin, Empty, Progress, Tag, Typography } from '@douyinfe/semi-ui';
import { IconChevronDown, IconChevronUp, IconRefresh } from '@douyinfe/semi-icons';
import { API, showError } from '../../../helpers';

const { Text } = Typography;

const fmtTs = (ts) => (ts ? new Date(ts * 1000).toLocaleDateString() : '-');

// 各中心 Token 占比（近 30 天）：数据来自零信任同步的「所在中心」（users.center），
// 中心为空的用户归入「未同步中心」。仅管理员可见（接口本身也是 AdminAuth）。
const CenterStatsPanel = () => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState(null);
  const [range, setRange] = useState(null);
  const [isAdmin, setIsAdmin] = useState(false);

  useEffect(() => {
    try {
      const u = JSON.parse(localStorage.getItem('user') || '{}');
      setIsAdmin((u.role ?? 0) >= 10);
    } catch (e) {
      setIsAdmin(false);
    }
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/log/center_stats');
      if (res.data.success) {
        setItems(res.data.data.items || []);
        setRange({
          start: res.data.data.start_timestamp,
          end: res.data.data.end_timestamp,
        });
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.message);
    }
    setLoading(false);
  }, []);

  useEffect(() => {
    if (open && isAdmin && items === null && !loading) {
      load();
    }
  }, [open, isAdmin]); // eslint-disable-line

  const totals = useMemo(() => {
    if (!items) return { tokens: 0, cost: 0, count: 0, centers: 0 };
    return items.reduce(
      (acc, r) => ({
        tokens: acc.tokens + (r.total_tokens || 0),
        cost: acc.cost + (r.cost_cny || 0),
        count: acc.count + (r.count || 0),
        centers: acc.centers + 1,
      }),
      { tokens: 0, cost: 0, count: 0, centers: 0 },
    );
  }, [items]);

  if (!isAdmin) return null;

  const columns = [
    {
      title: t('中心'),
      dataIndex: 'center',
      width: 200,
      render: (v, r) => (
        <div className='flex items-center gap-2'>
          <span>{v}</span>
          {r.users > 0 && (
            <Tag size='small' color='grey'>
              {r.users} {t('人')}
            </Tag>
          )}
        </div>
      ),
    },
    {
      title: t('Token 占比'),
      dataIndex: 'percent',
      width: 260,
      render: (v, r) => (
        <div className='flex items-center gap-2'>
          <Progress
            percent={Math.min(100, Number(v || 0))}
            showWarning={false}
            style={{ width: 150 }}
            aria-label='token-percent'
          />
          <Text size='small' type='tertiary'>
            {(Number(v) || 0).toFixed(1)}%
          </Text>
        </div>
      ),
    },
    {
      title: t('总 Tokens'),
      dataIndex: 'total_tokens',
      render: (v) => Number(v || 0).toLocaleString(),
    },
    {
      title: t('输入 / 输出'),
      dataIndex: 'prompt_tokens',
      render: (v, r) =>
        `${Number(v || 0).toLocaleString()} / ${Number(r.completion_tokens || 0).toLocaleString()}`,
    },
    {
      title: t('调用次数'),
      dataIndex: 'count',
      render: (v) => Number(v || 0).toLocaleString(),
    },
    {
      title: t('费用(元)'),
      dataIndex: 'cost_cny',
      render: (v) => `¥${(Number(v) || 0).toFixed(2)}`,
    },
  ];

  return (
    <div style={{ marginTop: 16 }}>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: 8,
        }}
      >
        <Button
          theme='borderless'
          icon={open ? <IconChevronUp /> : <IconChevronDown />}
          onClick={() => setOpen((v) => !v)}
        >
          {t('各中心 Token 占比（近 30 天）')}
        </Button>
        {open && (
          <Button size='small' theme='borderless' icon={<IconRefresh />} onClick={load} loading={loading}>
            {t('刷新')}
          </Button>
        )}
      </div>
      {open && (
        <Spin spinning={loading}>
          <div style={{ marginTop: 8 }}>
            {items && items.length > 0 ? (
              <>
                <Text type='tertiary' size='small'>
                  {t('区间：{{start}} ~ {{end}}，共 {{centers}} 个中心，合计 {{tokens}} tokens（¥{{cost}}）。中心来自零信任组织架构同步。', {
                    start: fmtTs(range?.start),
                    end: fmtTs(range?.end),
                    centers: totals.centers,
                    tokens: totals.tokens.toLocaleString(),
                    cost: totals.cost.toFixed(2),
                  })}
                </Text>
                <Table
                  size='small'
                  columns={columns}
                  dataSource={items}
                  rowKey='center'
                  pagination={false}
                />
              </>
            ) : (
              !loading && <Empty title={t('暂无数据（需先通过零信任同步用户所在中心）')} />
            )}
          </div>
        </Spin>
      )}
    </div>
  );
};

export default CenterStatsPanel;
