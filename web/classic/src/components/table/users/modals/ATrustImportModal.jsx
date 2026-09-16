import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Modal,
  Table,
  Button,
  Input,
  InputNumber,
  Typography,
  Tag,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../../../helpers';

const { Text } = Typography;

// 零信任目录预建用户：SSO 登录前配置分组与额度（登录按工号命中预建账号）
const ATrustImportModal = ({ visible, handleClose, refresh, groupOptions }) => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [importing, setImporting] = useState(false);
  const [keyword, setKeyword] = useState('');
  const [onlyNew, setOnlyNew] = useState(true);
  const [users, setUsers] = useState([]);
  const [selectedKeys, setSelectedKeys] = useState([]);
  const [group, setGroup] = useState('default');
  const [quotaCNY, setQuotaCNY] = useState(0);
  const [result, setResult] = useState(null);

  const fetchDir = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      if (keyword) params.set('keyword', keyword);
      if (onlyNew) params.set('only_new', 'true');
      const res = await API.get(`/api/user/atrust_directory?${params}`);
      if (res.data.success) {
        setUsers(res.data.data.users || []);
        setSelectedKeys([]);
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('获取失败'));
    }
    setLoading(false);
  }, [keyword, onlyNew, t]);

  useEffect(() => {
    if (visible && users.length === 0 && !result) {
      fetchDir();
    }
  }, [visible]); // eslint-disable-line

  const doImport = async () => {
    if (selectedKeys.length === 0) {
      showError(t('请先勾选要导入的用户'));
      return;
    }
    setImporting(true);
    try {
      const items = users
        .filter((u) => selectedKeys.includes(u.employee_id))
        .map((u) => ({
          employee_id: u.employee_id,
          display_name: u.display_name,
          group,
          quota_cny: quotaCNY || 0,
        }));
      const res = await API.post('/api/user/import_atrust', { users: items });
      if (res.data.success) {
        const d = res.data.data;
        setResult(d);
        showSuccess(
          t('导入完成') +
            `：${t('成功')} ${d.success_count} / ${t('跳过')} ${d.duplicate_count} / ${t('失败')} ${d.error_count}`,
        );
        refresh();
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('操作失败'));
    }
    setImporting(false);
  };

  const columns = useMemo(
    () => [
      {
        title: t('姓名'),
        dataIndex: 'display_name',
        width: 120,
        render: (v) => <Text strong>{v}</Text>,
      },
      { title: t('工号'), dataIndex: 'employee_id', width: 130 },
      {
        title: t('状态'),
        dataIndex: 'exists_local',
        width: 120,
        render: (v, r) =>
          v ? (
            <Tag color='grey' size='small'>
              {t('已存在')}·{r.matched_user}
            </Tag>
          ) : (
            <Tag color='green' size='small'>
              {t('可导入')}
            </Tag>
          ),
      },
    ],
    [t],
  );

  const resultColumns = useMemo(
    () => [
      { title: t('姓名'), dataIndex: 'display_name', width: 110 },
      { title: t('工号'), dataIndex: 'employee_id', width: 120 },
      {
        title: t('结果'),
        dataIndex: 'status',
        width: 90,
        render: (v) =>
          v === 'success' ? (
            <Tag color='green' size='small'>{t('成功')}</Tag>
          ) : v === 'duplicate' ? (
            <Tag color='amber' size='small'>{t('跳过')}</Tag>
          ) : (
            <Tag color='red' size='small'>{t('失败')}</Tag>
          ),
      },
      { title: '', dataIndex: 'message' },
    ],
    [t],
  );

  const onClose = () => {
    setResult(null);
    handleClose();
  };

  return (
    <Modal
      title={t('从零信任目录导入用户')}
      visible={visible}
      onCancel={onClose}
      footer={null}
      width={680}
    >
      {result ? (
        <>
          <Text type='tertiary' size='small' style={{ display: 'block', marginBottom: 8 }}>
            {t('导入完成') +
              `：${t('成功')} ${result.success_count} / ${t('跳过')} ${result.duplicate_count} / ${t('失败')} ${result.error_count}`}
          </Text>
          <Table
            size='small'
            columns={resultColumns}
            dataSource={result.results}
            rowKey={(r) => r.employee_id + r.display_name}
            pagination={false}
            maxHeight={320}
          />
          <div style={{ marginTop: 12, textAlign: 'right' }}>
            <Button theme='solid' onClick={onClose}>
              {t('完成')}
            </Button>
          </div>
        </>
      ) : (
        <>
          <Text type='tertiary' size='small' style={{ display: 'block', marginBottom: 10 }}>
            {t('预建账号后，员工首次零信任登录将按工号命中该账号，分组与额度即刻生效（无需登录后再调整）')}
          </Text>
          <div
            style={{
              display: 'flex',
              gap: 10,
              marginBottom: 10,
              flexWrap: 'wrap',
              alignItems: 'center',
            }}
          >
            <Input
              style={{ width: 180 }}
              placeholder={t('搜索姓名/工号')}
              value={keyword}
              onChange={setKeyword}
              showClear
            />
            <Button loading={loading} onClick={fetchDir}>
              {t('查询')}
            </Button>
            <label style={{ fontSize: 13, display: 'flex', alignItems: 'center', gap: 4 }}>
              <input
                type='checkbox'
                checked={onlyNew}
                onChange={(e) => setOnlyNew(e.target.checked)}
              />
              {t('仅显示新用户')}
            </label>
          </div>
          <Table
            size='small'
            columns={columns}
            dataSource={users}
            rowKey='employee_id'
            loading={loading}
            pagination={{ pageSize: 10 }}
            rowSelection={{
              selectedRowKeys: selectedKeys,
              onChange: setSelectedKeys,
              getCheckboxProps: (r) => ({ disabled: r.exists_local }),
            }}
            maxHeight={280}
          />
          <div
            style={{
              display: 'flex',
              gap: 10,
              marginTop: 12,
              alignItems: 'center',
              flexWrap: 'wrap',
            }}
          >
            <span style={{ fontSize: 13 }}>
              {t('已选')} {selectedKeys.length} {t('人')}，{t('导入分组')}：
            </span>
            <select
              value={group}
              onChange={(e) => setGroup(e.target.value)}
              style={{ padding: '4px 8px', borderRadius: 4, border: '1px solid #d9d9d9' }}
            >
              {(groupOptions || []).map((g) => (
                <option key={g.value} value={g.value}>
                  {g.label}
                </option>
              ))}
            </select>
            <span style={{ fontSize: 13 }}>{t('额度')}（{t('元')}）：</span>
            <InputNumber
              value={quotaCNY}
              onChange={setQuotaCNY}
              min={0}
              style={{ width: 120 }}
              placeholder={t('默认额度')}
            />
            <Button
              theme='solid'
              loading={importing}
              disabled={selectedKeys.length === 0}
              onClick={doImport}
            >
              {t('导入')} {selectedKeys.length > 0 ? `(${selectedKeys.length})` : ''}
            </Button>
          </div>
        </>
      )}
    </Modal>
  );
};

export default ATrustImportModal;
