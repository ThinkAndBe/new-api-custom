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
      title: t('字数'),
      key: 'content_length',
      width: 80,
      sorter: (a, b) => (a.request_content?.length || 0) - (b.request_content?.length || 0),
      render: (_, record) => record.request_content ? record.request_content.length : 0,
    },
  ], [t]);

  const expandRowRender = (record) => {
    if (!record.request_content) return null;
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
      </div>
    );
  };

  return (
    <div className='px-4 py-2'>
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
    </div>
  );
};

export default ChatLog;
