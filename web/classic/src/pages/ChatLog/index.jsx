import React, { useState, useEffect, useCallback, useMemo } from 'react';
import {
  Card,
  Table,
  Tag,
  Button,
  Input,
  Form,
  Modal,
  Typography,
  Space,
  Popconfirm,
  Banner,
} from '@douyinfe/semi-ui';
import {
  IconDownload,
  IconDelete,
  IconRefresh,
  IconSearch,
} from '@douyinfe/semi-icons';
import CardTable from '../../components/common/ui/CardTable';
import { API, showError, showSuccess, timestamp2string, copy } from '../../helpers';
import { DATE_RANGE_PRESETS } from '../../constants/console.constants';
import { exportFromAPI, genExportFilename } from '../../helpers/csv';
import { useTranslation } from 'react-i18next';

const { Text, Title } = Typography;

const ChatLog = () => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [logs, setLogs] = useState([]);

  const copyText = async (e, text) => {
    e.stopPropagation();
    if (await copy(text)) {
      showSuccess('已复制：' + text);
    }
  };
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [expandedRowKeys, setExpandedRowKeys] = useState([]);
  const [formApi, setFormApi] = useState(null);
  const [exporting, setExporting] = useState(false);
  const [userStats, setUserStats] = useState([]);
  const [showUserStats, setShowUserStats] = useState(false);

  const handleUserStats = async () => {
    try {
      const formData = formApi?.getValues() || {};
      const params = new URLSearchParams();
      if (formData.username) params.set('username', formData.username);
      if (formData.model_name) params.set('model_name', formData.model_name);
      if (formData.group) params.set('group', formData.group);
      if (formData.dateRange && formData.dateRange.length === 2) {
        params.set('start_timestamp', String(Math.floor(formData.dateRange[0].getTime() / 1000)));
        params.set('end_timestamp', String(Math.floor(formData.dateRange[1].getTime() / 1000)));
      }
      // 汇总取真实账目（logs 表，历史完整）；chat_logs 的 token 列仅覆盖开启后的新对话
      params.set('type', '2');
      const res = await API.get(`/api/log/user_stats?${params.toString()}`);
      const { success, data, message } = res.data;
      if (success) {
        setUserStats(data || []);
        setShowUserStats(true);
      } else {
        showError(message || t('获取失败'));
      }
    } catch (err) {
      showError(err.response?.data?.message || t('获取失败'));
    }
  };

  const fetchLogs = useCallback(async () => {
    setLoading(true);
    try {
      const formData = formApi?.getValues() || {};
      const params = new URLSearchParams();
      params.set('p', String(page));
      params.set('page_size', String(pageSize));
      if (formData.username) params.set('username', formData.username);
      if (formData.model_name) params.set('model_name', formData.model_name);
      if (formData.group) params.set('group', formData.group);
      if (formData.dateRange && formData.dateRange.length === 2) {
        params.set('start_timestamp', String(Math.floor(formData.dateRange[0].getTime() / 1000)));
        params.set('end_timestamp', String(Math.floor(formData.dateRange[1].getTime() / 1000)));
      }
      const res = await API.get(`/api/chat_log/?${params.toString()}`);
      const { success, data } = res.data;
      if (success) {
        setLogs(data.items || []);
        setTotal(data.total || 0);
      } else {
        showError(data.message || t('获取失败'));
      }
    } catch (err) {
      showError(err.response?.data?.message || t('获取失败'));
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, formApi, t]);

  useEffect(() => {
    fetchLogs();
  }, [page, pageSize, fetchLogs]);

  const handleSearch = () => {
    setPage(1);
    fetchLogs();
  };

  const handleExport = async () => {
    if (exporting) return;
    setExporting(true);
    try {
      const formData = formApi?.getValues() || {};
      const params = new URLSearchParams();
      if (formData.username) params.set('username', formData.username);
      if (formData.model_name) params.set('model_name', formData.model_name);
      if (formData.group) params.set('group', formData.group);
      if (formData.dateRange && formData.dateRange.length === 2) {
        params.set('start_timestamp', String(Math.floor(formData.dateRange[0].getTime() / 1000)));
        params.set('end_timestamp', String(Math.floor(formData.dateRange[1].getTime() / 1000)));
      }
      const url = `/api/chat_log/export?${params.toString()}`;
      const filename = genExportFilename('chat_logs', 'csv');
      await exportFromAPI(url, filename);
      showSuccess(t('导出成功'));
    } catch (err) {
      showError(err.message || t('导出失败'));
    } finally {
      setExporting(false);
    }
  };

  const handleDeleteAll = async () => {
    try {
      const res = await API.delete('/api/chat_log/');
      if (res.data.success) {
        showSuccess(t('已清空所有对话日志'));
        fetchLogs();
      } else {
        showError(res.data.message);
      }
    } catch (err) {
      showError(t('操作失败'));
    }
  };

  const columns = useMemo(() => [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 70,
    },
    {
      title: t('时间'),
      dataIndex: 'created_at',
      width: 160,
      render: (val) => (
        <Text style={{ fontSize: 12 }}>{timestamp2string(val)}</Text>
      ),
    },
    {
      title: t('用户'),
      dataIndex: 'username',
      width: 140,
      render: (val, record) => (
        <div>
          <span
            style={{ cursor: 'pointer' }}
            onClick={(e) => copyText(e, val)}
          >
            {val || '-'}
          </span>
          <div style={{ fontSize: 11, color: 'var(--semi-color-text-2)' }}>
            ID: {record.user_id}
          </div>
        </div>
      ),
    },
    {
      title: t('模型'),
      dataIndex: 'model_name',
      width: 160,
      render: (val) => val ? (
        <Tag
          color='blue'
          size='small'
          style={{ cursor: 'pointer' }}
          onClick={(e) => copyText(e, val)}
        >
          {val}
        </Tag>
      ) : '-',
    },
    {
      title: t('令牌'),
      dataIndex: 'token_name',
      width: 120,
      render: (val) => val ? (
        <Text style={{ fontFamily: 'monospace', fontSize: 12 }}>
          {val.length > 12 ? val.slice(0, 8) + '...' + val.slice(-4) : val}
        </Text>
      ) : '-',
    },
    {
      title: t('渠道'),
      dataIndex: 'channel_id',
      width: 80,
      render: (val) => val ? `#${val}` : '-',
    },
    {
      title: t('分组'),
      dataIndex: 'group',
      width: 100,
      render: (val) => val ? (
        <Tag
          color='cyan'
          size='small'
          style={{ cursor: 'pointer' }}
          onClick={(e) => copyText(e, val)}
        >
          {val}
        </Tag>
      ) : '-',
    },
    {
      title: t('内容预览'),
      dataIndex: 'request_content',
      render: (val) => {
        if (!val) return <Text type='tertiary'>{t('无内容')}</Text>;
        const preview = val.length > 120 ? val.slice(0, 120) + '...' : val;
        return (
          <Text style={{ maxWidth: 500, cursor: 'pointer' }} ellipsis={{ showTooltip: true }}>
            {preview}
          </Text>
        );
      },
    },
    {
      title: t('输出预览'),
      dataIndex: 'response_content',
      render: (val) => {
        if (!val) return <Text type='tertiary'>{t('暂无')}</Text>;
        const preview = val.length > 120 ? val.slice(0, 120) + '...' : val;
        return (
          <Text style={{ maxWidth: 400, cursor: 'pointer' }} ellipsis={{ showTooltip: true }}>
            {preview}
          </Text>
        );
      },
    },
    {
      title: t('字数'),
      key: 'content_length',
      width: 80,
      sorter: (a, b) => (a.request_content?.length || 0) - (b.request_content?.length || 0),
      render: (_, record) => record.request_content ? record.request_content.length : 0,
    },
  ], [t]);

  const expandRowRender = (record) => {
    if (!record.request_content && !record.response_content) return null;
    const segments = record.request_content.split('\n[');
    const roleColors = {
      system: 'orange',
      user: 'blue',
      assistant: 'green',
      tool: 'purple',
    };
    return (
      <div style={{ padding: 16, background: 'var(--semi-color-fill-0)', borderRadius: 8 }}>
        {segments.map((seg, idx) => {
          let role = '';
          let content = seg;
          if (idx === 0 && !seg.startsWith('[')) {
            role = 'message';
          } else {
            const match = seg.match(/^(\w+)\]\s*(.*)/s);
            if (match) {
              role = match[1];
              content = match[2];
            }
          }
          return (
            <div key={idx} style={{ marginBottom: idx < segments.length - 1 ? 12 : 0 }}>
              {role && (
                <Tag color={roleColors[role] || 'grey'} size='small' style={{ marginRight: 8 }}>
                  {role}
                </Tag>
              )}
              <div style={{
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
                fontSize: 13,
                lineHeight: 1.6,
                maxHeight: 500,
                overflow: 'auto',
                marginTop: 4,
                padding: '8px 12px',
                background: 'var(--semi-color-bg-1)',
                borderRadius: 6,
              }}>
                {content}
              </div>
            </div>
          );
        })}
        {record.response_content && (
          <div style={{ marginTop: 16 }}>
            <Tag color='green' size='small'>assistant(输出)</Tag>
            <div style={{
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-all',
              fontSize: 13,
              lineHeight: 1.6,
              maxHeight: 500,
              overflow: 'auto',
              marginTop: 4,
              padding: '8px 12px',
              background: 'var(--semi-color-success-light-default)',
              borderRadius: 6,
            }}>
              {record.response_content}
            </div>
          </div>
        )}
      </div>
    );
  };

  return (
    <div className='mt-[60px] px-4 py-2'>
      <Card>
        <div className='flex items-center justify-between mb-4'>
          <div>
            <Title heading={5} style={{ marginBottom: 0 }}>{t('对话日志')}</Title>
            <Text type='tertiary' size='small'>
              {t('用户请求内容审计记录 · 共 ')} {total} {t(' 条')}
            </Text>
          </div>
        </div>

        <Form
          getFormApi={(api) => setFormApi(api)}
          onSubmit={handleSearch}
          allowEmpty
          autoComplete='off'
          labelPosition='inset'
        >
          <div className='flex flex-wrap items-center gap-2 mb-3'>
            <div style={{ width: 360 }}>
              <Form.DatePicker
                field='dateRange'
                label={t('时间范围')}
                className='w-full'
                type='dateTimeRange'
                placeholder={[t('开始时间'), t('结束时间')]}
                showClear
                density='compact'
                presets={DATE_RANGE_PRESETS.map((preset) => ({
                  text: t(preset.text),
                  start: preset.start(),
                  end: preset.end(),
                }))}
              />
            </div>
            <Form.Input
              field='username'
              label={t('用户')}
              placeholder={t('用户名')}
              showClear
              density='compact'
              style={{ width: 150 }}
            />
            <Form.Input
              field='model_name'
              label={t('模型')}
              placeholder={t('模型名称')}
              showClear
              density='compact'
              style={{ width: 150 }}
            />
            <Form.Input
              field='group'
              label={t('分组')}
              placeholder={t('分组')}
              showClear
              density='compact'
              style={{ width: 150 }}
            />
          </div>
          <div className='flex gap-2 mb-3'>
            <Button htmlType='submit' theme='solid' icon={<IconSearch />} loading={loading}>
              {t('查询')}
            </Button>
            <Button
              icon={<IconRefresh />}
              onClick={() => { formApi?.setValues({}); setPage(1); fetchLogs(); }}
            >
              {t('重置')}
            </Button>
            <Button icon={<IconDownload />} loading={exporting} onClick={handleExport}>
              {t('导出CSV')}
            </Button>
            <Button onClick={handleUserStats}>{t('按用户汇总')}</Button>
            <Popconfirm
              title={t('确认清空')}
              content={t('确定要清空所有对话日志吗？此操作不可撤销。')}
              onConfirm={handleDeleteAll}
            >
              <Button icon={<IconDelete />} type='danger'>{t('清空全部')}</Button>
            </Popconfirm>
          </div>
        </Form>

        {total === 0 && !loading && (
          <Banner
            type='info'
            description={t('暂无对话日志。请在「设置 → 运营设置」中开启「对话日志记录」功能。')}
            style={{ marginBottom: 16 }}
          />
        )}

        <CardTable
          columns={columns}
          dataSource={logs}
          rowKey='id'
          loading={loading}
          pagination={{
            currentPage: page,
            pageSize: pageSize,
            total: total,
            onPageChange: (p) => setPage(p),
            onPageSizeChange: (s) => { setPageSize(s); setPage(1); },
            showSizeChanger: true,
            pageSizeOpts: [10, 20, 50, 100],
          }}
          expandRowKeys={expandedRowKeys}
          onExpand={(isExpand, record) => {
            if (isExpand) {
              setExpandedRowKeys([...expandedRowKeys, record.id]);
            } else {
              setExpandedRowKeys(expandedRowKeys.filter((k) => k !== record.id));
            }
          }}
          expandedRowRender={expandRowRender}
          rowExpandable={(record) => record.request_content}
        />
      </Card>

      <Modal
        title={t('按用户汇总（真实账目：token 用量与调用次数）')}
        visible={showUserStats}
        onCancel={() => setShowUserStats(false)}
        footer={
          <Button
            icon={<IconDownload />}
            disabled={!userStats.length}
            onClick={() => {
              const header = ['用户名', '调用次数', '输入Tokens', '输出Tokens', '总Tokens', '花费(元)'];
              const rows = userStats.map((r) => [
                r.username || '',
                r.count || 0,
                r.prompt_tokens || 0,
                r.completion_tokens || 0,
                (r.prompt_tokens || 0) + (r.completion_tokens || 0),
                ((r.quota || 0) / 500000).toFixed(6),
              ]);
              const csv = [header, ...rows]
                .map((cols) => cols.map((c) => `"${String(c).replace(/"/g, '""')}"`).join(','))
                .join(String.fromCharCode(10));
              const blob = new Blob([String.fromCharCode(0xfeff) + csv], { type: 'text/csv;charset=utf-8' });
              const url = URL.createObjectURL(blob);
              const a = document.createElement('a');
              a.href = url;
              a.download = `用户用量汇总_${new Date().toISOString().slice(0, 10)}.csv`;
              a.click();
              URL.revokeObjectURL(url);
            }}
          >
            {t('导出CSV')}
          </Button>
        }
        width={680}
      >
        <Table
          size='small'
          dataSource={userStats}
          rowKey='user_id'
          pagination={userStats.length > 20 ? { pageSize: 20 } : false}
          columns={[
            { title: t('用户名'), dataIndex: 'username', width: 140 },
            {
              title: t('调用次数'),
              dataIndex: 'count',
              width: 100,
              render: (v) => (v != null ? Number(v).toLocaleString() : '-'),
            },
            {
              title: t('输入 Tokens'),
              dataIndex: 'prompt_tokens',
              render: (v) => (v != null ? Number(v).toLocaleString() : '-'),
            },
            {
              title: t('输出 Tokens'),
              dataIndex: 'completion_tokens',
              render: (v) => (v != null ? Number(v).toLocaleString() : '-'),
            },
            {
              title: t('总 Tokens'),
              render: (_, r) =>
                Number((r.prompt_tokens || 0) + (r.completion_tokens || 0)).toLocaleString(),
            },
            {
              title: t('花费'),
              dataIndex: 'quota',
              width: 100,
              render: (v) => (v != null ? '¥' + (v / 500000).toFixed(2) : '-'),
            },
          ]}
        />
      </Modal>
    </div>
  );
};

export default ChatLog;
