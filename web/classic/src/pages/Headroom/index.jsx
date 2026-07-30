import React, { useEffect, useState, useCallback } from 'react';
import { Banner, Button, Card, DatePicker, Spin, Table, Typography } from '@douyinfe/semi-ui';
import { VChart } from '@visactor/react-vchart';
import { useTranslation } from 'react-i18next';
import { API, renderNumber, timestamp2string } from '../../helpers';
import { downloadCSV } from '../../helpers/csv';

const { Title, Text } = Typography;

/**
 * 计算预设时间范围的 [startTs, endTs]（秒级）
 * - today: 今日 0:00 ~ 现在
 * - yesterday: 昨日 0:00 ~ 昨日 23:59:59
 * - 7d: 最近 7 天 ~ 现在
 * - 30d: 最近 30 天 ~ 现在
 * - all: 不过滤（返回 null）
 */
const computeRange = (preset) => {
  const now = new Date();
  const endTs = Math.floor(now.getTime() / 1000);
  const startOfDay = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  switch (preset) {
    case 'today':
      return [Math.floor(startOfDay.getTime() / 1000), endTs];
    case 'yesterday': {
      const startY = new Date(startOfDay.getTime() - 24 * 3600 * 1000);
      const endY = new Date(startOfDay.getTime() - 1000);
      return [Math.floor(startY.getTime() / 1000), Math.floor(endY.getTime() / 1000)];
    }
    case '7d':
      return [Math.floor(now.getTime() / 1000) - 7 * 24 * 3600, endTs];
    case '30d':
      return [Math.floor(now.getTime() / 1000) - 30 * 24 * 3600, endTs];
    case 'all':
    default:
      return null;
  }
};

// AggTable 月度/年度汇总表：表格 + 合计行（参考 TokenSummaryPanel 模式）。
const AggTable = ({ title, data, periodLabel }) => {
  const { t } = useTranslation();
  const columns = [
    { title: periodLabel, dataIndex: 'time' },
    { title: t('节省 Tokens'), dataIndex: 'tokens_saved', render: renderNumber, sorter: (a, b) => a.tokens_saved - b.tokens_saved, defaultSortOrder: 'descend' },
    { title: t('原输入 Tokens'), dataIndex: 'tokens_input', render: renderNumber, sorter: (a, b) => a.tokens_input - b.tokens_input },
    { title: t('实际发送'), dataIndex: 'tokens_compressed', render: renderNumber, sorter: (a, b) => a.tokens_compressed - b.tokens_compressed },
    { title: t('请求数'), dataIndex: 'request_count', render: renderNumber, sorter: (a, b) => a.request_count - b.request_count },
    { title: t('节省率'), dataIndex: 'average_ratio', render: (v) => `${((v || 0) * 100).toFixed(1)}%`, sorter: (a, b) => a.average_ratio - b.average_ratio },
  ];
  const totals = (data || []).reduce((acc, r) => {
    acc.saved += r.tokens_saved || 0;
    acc.input += r.tokens_input || 0;
    acc.compressed += r.tokens_compressed || 0;
    acc.count += r.request_count || 0;
    return acc;
  }, { saved: 0, input: 0, compressed: 0, count: 0 });
  const totalRatio = totals.input > 0 ? totals.saved / totals.input : 0;
  return (
    <Card className='!rounded-2xl'>
      <Title heading={5} style={{ marginBottom: 12 }}>{title}</Title>
      <Table
        columns={columns}
        dataSource={data}
        rowKey='time'
        size='small'
        pagination={data && data.length > 12 ? { pageSize: 12, size: 'small' } : false }
      />
      <div className='mt-3 p-2 rounded' style={{ background: 'var(--semi-color-fill-1)' }}>
        <Text strong>{t('合计')}</Text>
        <span className='ml-3'>{t('节省')}: <strong>{renderNumber(totals.saved)}</strong></span>
        <span className='ml-3'>{t('原输入')}: <strong>{renderNumber(totals.input)}</strong></span>
        <span className='ml-3'>{t('请求数')}: <strong>{renderNumber(totals.count)}</strong></span>
        <span className='ml-3'>{t('节省率')}: <strong>{(totalRatio * 100).toFixed(1)}%</strong></span>
      </div>
    </Card>
  );
};

const HeadroomDashboard = () => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [summary, setSummary] = useState({});
  const [byModel, setByModel] = useState([]);
  const [byUser, setByUser] = useState([]);
  const [byChannel, setByChannel] = useState([]);
  const [byChannelModel, setByChannelModel] = useState([]);
  const [recent, setRecent] = useState([]);
  const [recentTotal, setRecentTotal] = useState(0);
  const [recentPage, setRecentPage] = useState(1);
  const [recentPageSize, setRecentPageSize] = useState(20);

  // 历史趋势数据
  const [trend, setTrend] = useState([]);
  const [trendGranularity, setTrendGranularity] = useState('day');

  // 月度/年度汇总
  const [monthly, setMonthly] = useState([]);
  const [yearly, setYearly] = useState([]);

  // 时间范围：使用和数剧看板一致的时间戳字符串格式
  // appliedStart/appliedEnd 是当前生效的（用于查询），pending 是选择中的
  const [appliedStart, setAppliedStart] = useState(() => timestamp2string(computeRange('today')[0]));
  const [appliedEnd, setAppliedEnd] = useState(() => timestamp2string(computeRange('today')[1]));
  const [pendingStart, setPendingStart] = useState(appliedStart);
  const [pendingEnd, setPendingEnd] = useState(appliedEnd);

  // 计算查询参数
  const buildParams = useCallback(() => {
    const params = {};
    if (appliedStart) {
      params.start_timestamp = Math.floor(Date.parse(appliedStart) / 1000);
    }
    if (appliedEnd) {
      params.end_timestamp = Math.floor(Date.parse(appliedEnd) / 1000);
    }
    return params;
  }, [appliedStart, appliedEnd]);

  const loadAggData = useCallback(async () => {
    const params = buildParams();
    const [s, m, u, c, cm, tr, mo, yr] = await Promise.all([
      API.get('/api/data/headroom/summary', { params }),
      API.get('/api/data/headroom/by_model', { params }),
      API.get('/api/data/headroom/by_user', { params }),
      API.get('/api/data/headroom/by_channel', { params }),
      API.get('/api/data/headroom/by_channel_model', { params }),
      API.get('/api/data/headroom/trend', { params }),
      // 月度/年度汇总独立于时间范围，始终返回全部历史
      API.get('/api/data/headroom/monthly'),
      API.get('/api/data/headroom/yearly'),
    ]);
    if (s.data.success) setSummary(s.data.data || {});
    if (m.data.success) setByModel(m.data.data || []);
    if (u.data.success) setByUser(u.data.data || []);
    if (c.data.success) setByChannel(c.data.data || []);
    if (cm.data.success) setByChannelModel(cm.data.data || []);
    if (tr.data.success) {
      setTrend(tr.data.data || []);
      setTrendGranularity(tr.data.granularity || 'day');
    }
    if (mo.data.success) setMonthly(mo.data.data || []);
    if (yr.data.success) setYearly(yr.data.data || []);
  }, [buildParams]);

  const loadRecentData = useCallback(async (page, pageSize) => {
    const params = { ...buildParams(), page, page_size: pageSize };
    const r = await API.get('/api/data/headroom/recent', { params });
    if (r.data.success) {
      setRecent(r.data.data || []);
      setRecentTotal(r.data.total || 0);
    }
  }, [buildParams]);

  // appliedStart/appliedEnd 变化时重新加载全部数据
  useEffect(() => {
    const loadAll = async () => {
      setLoading(true);
      try {
        await loadAggData();
        await loadRecentData(1, recentPageSize);
        setRecentPage(1);
      } finally {
        setLoading(false);
      }
    };
    loadAll();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [appliedStart, appliedEnd]);

  const handlePageChange = async (page, pageSize) => {
    setRecentPage(page);
    if (pageSize !== recentPageSize) {
      setRecentPageSize(pageSize);
    }
    setLoading(true);
    try {
      await loadRecentData(page, pageSize);
    } finally {
      setLoading(false);
    }
  };

  // 点击”查询”按钮：将 pending 时间应用到查询
  const handleQuery = () => {
    setAppliedStart(pendingStart);
    setAppliedEnd(pendingEnd);
  };

  // 快捷预设
  const applyPreset = (preset) => {
    const range = computeRange(preset);
    if (range && range.length === 2) {
      const startStr = timestamp2string(range[0]);
      const endStr = timestamp2string(range[1]);
      setPendingStart(startStr);
      setPendingEnd(endStr);
      setAppliedStart(startStr);
      setAppliedEnd(endStr);
    }
  };

  const handleExport = async (view) => {
    const params = buildParams();
    const qs = new URLSearchParams({ view, ...params }).toString();
    try {
      const res = await API.get(`/api/data/headroom/export?${qs}`, { responseType: 'blob' });
      const blob = new Blob([res.data], { type: 'text/csv;charset=utf-8;' });
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `headroom_${view}_${Date.now()}.csv`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      window.URL.revokeObjectURL(url);
    } catch (e) {
      console.error('export failed', e);
    }
  };

  const handleExportCSV = () => {
    const headers = ['时间', '用户', '令牌', '模型', '渠道', '节省Tokens', '原输入Tokens', '压缩率', '请求ID'];
    const rows = [headers, ...recent.map((r) => [
      new Date(r.created_at * 1000).toLocaleString(),
      r.username || '',
      r.token_name || '',
      r.model_name || '',
      r.channel_name || `渠道#${r.channel_id}`,
      r.headroom_tokens_saved || 0,
      r.headroom_tokens_input || 0,
      `${((r.headroom_ratio || 0) * 100).toFixed(1)}%`,
      r.request_id || '',
    ])];
    downloadCSV(`headroom_detail_${Date.now()}.csv`, rows);
  };

  const makeRankSpec = (title, data) => ({
    type: 'bar',
    direction: 'horizontal',
    data: [{ id: 'data', values: (data || []).slice(0, 15) }],
    xField: 'tokens_saved',
    yField: 'name',
    seriesField: 'name',
    legends: { visible: false },
    title: { visible: true, text: title },
    label: { visible: true, position: 'outside', formatMethod: (v) => renderNumber(v || 0) },
    tooltip: {
      mark: {
        content: [
          { key: (d) => d.name, value: (d) => `${renderNumber(d.tokens_saved)} tokens` },
          { key: () => t('请求数'), value: (d) => renderNumber(d.request_count || 0) },
        ],
      },
    },
  });

  // 历史趋势图：堆叠面积图，tokens_compressed（实际发送）+ tokens_saved（节省）= tokens_input
  const granularityLabel = (g) => (g === 'month' ? t('按月') : g === 'week' ? t('按周') : t('按日'));
  const makeTrendSpec = (data, granularity) => ({
    type: 'area',
    stack: true,
    data: [{ id: 'trendData', values: (data || []) }],
    xField: 'time',
    yField: 'value',
    seriesField: 'type',
    legends: { visible: true, selectMode: 'single' },
    title: { visible: true, text: `${t('历史趋势')}（${granularityLabel(granularity)}）` },
    axes: [
      { orient: 'left', label: { formatMethod: (v) => renderNumber(v || 0) } },
      { orient: 'bottom', label: { autoLimit: false, autoHide: true } },
    ],
    area: { style: { fillOpacity: 0.25 } },
    line: { style: { lineWidth: 2 } },
    point: { visible: false },
    tooltip: {
      mark: {
        content: [
          { key: (d) => d.type, value: (d) => `${renderNumber(d.value)} tokens` },
          { key: () => t('请求数'), value: (d) => renderNumber(d.request_count || 0) },
          { key: () => t('节省率'), value: (d) => `${((d.average_ratio || 0) * 100).toFixed(1)}%` },
        ],
      },
    },
  });

  const columns = [
    { title: t('时间'), dataIndex: 'created_at', render: (v) => new Date(v * 1000).toLocaleString() },
    { title: t('用户'), dataIndex: 'username' },
    { title: t('模型'), dataIndex: 'model_name' },
    { title: t('渠道'), dataIndex: 'channel_name', render: (v, r) => v || `渠道#${r.channel_id}` },
    { title: t('原输入 Tokens'), dataIndex: 'headroom_tokens_input', render: renderNumber },
    { title: t('节省 Tokens'), dataIndex: 'headroom_tokens_saved', render: renderNumber },
    { title: t('实际输入 Tokens'), dataIndex: 'prompt_tokens', render: renderNumber },
    { title: t('输出 Tokens'), dataIndex: 'completion_tokens', render: renderNumber },
    { title: t('压缩率'), dataIndex: 'headroom_ratio', render: (v) => `${((v || 0) * 100).toFixed(1)}%` },
  ];

  // 当前生效范围的友好展示
  const currentRangeLabel = `${appliedStart || '?'} ~ ${appliedEnd || '?'}`;

  return (
    <div className='mt-[60px] px-2'>
      <Spin spinning={loading}>
        <div className='p-4 space-y-4'>
          <div className='flex items-center justify-between flex-wrap gap-4'>
            <Title heading={3}>{t('压缩看板')}</Title>
            <div className='flex items-center gap-2 flex-wrap'>
              <DatePicker
                type='dateTimeRange'
                density='compact'
                value={[pendingStart ? new Date(pendingStart) : null, pendingEnd ? new Date(pendingEnd) : null]}
                onChange={(dates) => {
                  if (dates && dates.length === 2) {
                    setPendingStart(dates[0] ? timestamp2string(Math.floor(dates[0].getTime() / 1000)) : '');
                    setPendingEnd(dates[1] ? timestamp2string(Math.floor(dates[1].getTime() / 1000)) : '');
                  }
                }}
                style={{ width: 380 }}
              />
              <Button size='small' onClick={() => applyPreset('today')}>{t('今天')}</Button>
              <Button size='small' onClick={() => applyPreset('yesterday')}>{t('昨天')}</Button>
              <Button size='small' onClick={() => applyPreset('7d')}>{t('最近 7 天')}</Button>
              <Button size='small' onClick={() => applyPreset('30d')}>{t('最近 30 天')}</Button>
              <Button size='small' theme='solid' type='primary' onClick={handleQuery}>
                {t('查询')}
              </Button>
              <Button size='small' onClick={handleExportCSV}>
                {t('导出明细 CSV')}
              </Button>
              <Button size='small' onClick={() => handleExport('model')}>
                {t('按模型导出')}
              </Button>
              <Button size='small' onClick={() => handleExport('channel')}>
                {t('按渠道导出')}
              </Button>
              <Button size='small' onClick={() => handleExport('monthly')}>
                {t('导出月度报表')}
              </Button>
              <Button size='small' onClick={() => handleExport('yearly')}>
                {t('导出年度报表')}
              </Button>
            </div>
          </div>
          <div>
            <Text type='tertiary' size='small'>
              {t('当前查询范围')}：{currentRangeLabel}
            </Text>
          </div>

          {/* 两维度独立展示：压缩 / 缓存，不做跨口径减法 */}
          <div className='grid grid-cols-1 md:grid-cols-2 gap-4'>
            {/* 维度1：压缩 */}
            <Card className='!rounded-xl'>
              <Text strong className='text-sm'>{t('① 压缩效益')}</Text>
              <div className='mt-2 space-y-1'>
                <div className='flex items-baseline justify-between'>
                  <Text type='tertiary' size='small'>{t('节省率')}</Text>
                  <span className='text-xl font-bold text-green-600'>{((summary.average_ratio || 0) * 100).toFixed(1)}%</span>
                </div>
                <div className='flex items-baseline justify-between'>
                  <Text type='tertiary' size='small'>{t('节省 Tokens')}</Text>
                  <span className='font-semibold'>{renderNumber(summary.tokens_saved || 0)}</span>
                </div>
                <div className='flex items-baseline justify-between'>
                  <Text type='tertiary' size='small'>{t('原输入')}</Text>
                  <span className='text-sm'>{renderNumber(summary.tokens_input || 0)}</span>
                </div>
                <div className='flex items-baseline justify-between'>
                  <Text type='tertiary' size='small'>{t('压缩后')}</Text>
                  <span className='text-sm'>{renderNumber(summary.tokens_compressed || 0)}</span>
                </div>
                <div className='flex items-baseline justify-between'>
                  <Text type='tertiary' size='small'>{t('请求数')}</Text>
                  <span className='text-sm'>{renderNumber(summary.request_count || 0)}</span>
                </div>
              </div>
            </Card>
            {/* 维度2：缓存命中 */}
            <Card className='!rounded-xl'>
              <Text strong className='text-sm'>{t('② 缓存命中')}</Text>
              <div className='mt-2 space-y-1'>
                <div className='flex items-baseline justify-between'>
                  <Text type='tertiary' size='small'>{t('命中率')}</Text>
                  <span className='text-xl font-bold text-blue-600'>{((summary.cache_hit_rate || 0) * 100).toFixed(1)}%</span>
                </div>
                <div className='flex items-baseline justify-between'>
                  <Text type='tertiary' size='small'>{t('命中 Tokens')}</Text>
                  <span className='font-semibold'>{renderNumber(summary.cache_hit_tokens || 0)}</span>
                </div>
                <div className='flex items-baseline justify-between'>
                  <Text type='tertiary' size='small'>{t('上游 Prompt')}</Text>
                  <span className='text-sm'>{renderNumber(summary.upstream_prompt || 0)}</span>
                </div>
              </div>
            </Card>
          </div>
          <Banner
            type='info'
            description={t('压缩是否有效看「①节省率」；压缩让重复内容更稳定命中缓存看「②命中率」。两者独立统计，不做跨口径换算。')}
            style={{ marginTop: 12 }}
          />

          <div className='grid grid-cols-1 lg:grid-cols-2 gap-4'>
            <Card className='!rounded-2xl'>
              <div className='h-96'>
                <VChart spec={makeRankSpec(t('按模型节省排行'), byModel)} />
              </div>
            </Card>
            <Card className='!rounded-2xl'>
              <div className='h-96'>
                <VChart spec={makeRankSpec(t('按渠道节省排行'), byChannel)} />
              </div>
            </Card>
          </div>

          {/* 渠道×模型：压缩节省率 + cache 命中率明细表 */}
          {byChannelModel.length > 0 && (
            <Card className='!rounded-2xl mt-4' title={t('渠道×模型 缓存命中率明细')}>
              <Table
                dataSource={byChannelModel}
                rowKey='name'
                pagination={{ pageSize: 15, showSizeChanger: true }}
                size='small'
                columns={[
                  { title: t('渠道'), dataIndex: 'channel_name', width: 180, sorter: (a,b) => a.channel_name?.localeCompare(b.channel_name) },
                  { title: t('模型'), dataIndex: 'model_name', width: 160, sorter: (a,b) => a.model_name?.localeCompare(b.model_name) },
                  { title: t('请求数'), dataIndex: 'request_count', width: 90, sorter: (a,b) => a.request_count - b.request_count, render: renderNumber },
                  { title: t('节省率'), dataIndex: 'average_ratio', width: 90, sorter: (a,b) => a.average_ratio - b.average_ratio, defaultSortOrder: 'descend', render: (v) => <span className='text-green-600 font-semibold'>{((v||0)*100).toFixed(1)}%</span> },
                  { title: t('缓存命中率'), dataIndex: 'cache_hit_rate', width: 100, sorter: (a,b) => a.cache_hit_rate - b.cache_hit_rate, render: (v) => <span className='text-blue-600 font-semibold'>{((v||0)*100).toFixed(1)}%</span> },
                  { title: t('命中 Tokens'), dataIndex: 'cache_hit_tokens', width: 120, sorter: (a,b) => a.cache_hit_tokens - b.cache_hit_tokens, render: renderNumber },
                  { title: t('节省 Tokens'), dataIndex: 'tokens_saved', width: 120, sorter: (a,b) => a.tokens_saved - b.tokens_saved, render: renderNumber },
                ]}
              />
            </Card>
          )}

          <Card className='!rounded-2xl' title={t('历史趋势')}>
            <div className='h-96'>
              <VChart
                spec={makeTrendSpec(
                  trend.flatMap((r) => [
                    { time: r.time, type: t('实际发送'), value: r.tokens_compressed || 0, request_count: r.request_count, average_ratio: r.average_ratio },
                    { time: r.time, type: t('节省'), value: r.tokens_saved || 0, request_count: r.request_count, average_ratio: r.average_ratio },
                  ]),
                  trendGranularity
                )}
              />
            </div>
          </Card>

          <Card className='!rounded-2xl'>
            <div className='h-96'>
              <VChart spec={makeRankSpec(t('按用户节省排行'), byUser)} />
            </div>
          </Card>

          {/* 月度/年度汇总表 */}
          <div className='grid grid-cols-1 lg:grid-cols-2 gap-4'>
            <AggTable title={t('月度汇总')} data={monthly} periodLabel={t('月份')} />
            <AggTable title={t('年度汇总')} data={yearly} periodLabel={t('年份')} />
          </div>

          <Card title={t('压缩记录明细')} className='!rounded-2xl'>
            <Table
              columns={columns}
              dataSource={recent}
              rowKey={(r, i) => `${r.request_id || ''}-${i}`}
              pagination={{
                currentPage: recentPage,
                pageSize: recentPageSize,
                total: recentTotal,
                pageSizeOptions: [20, 50, 100],
                showSizeChanger: true,
                onPageSizeChange: (size) => {
                  setRecentPageSize(size);
                  setRecentPage(1);
                  loadRecentData(1, size);
                },
                onPageChange: (page) => {
                  setRecentPage(page);
                  loadRecentData(page, recentPageSize);
                },
              }}
              size='small'
            />
          </Card>
        </div>
      </Spin>
    </div>
  );
};

export default HeadroomDashboard;
