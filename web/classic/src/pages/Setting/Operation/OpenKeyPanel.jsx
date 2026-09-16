import React, { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Table,
  Tag,
  Button,
  Modal,
  Form,
  Typography,
  Space,
  Popconfirm,
  Input,
  Switch,
  InputNumber,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../../helpers';

const { Text, Paragraph } = Typography;

// 数据开放：对话日志开放密钥管理（供顾问拉取做用户使用总结）
const OpenKeyPanel = () => {
  const { t } = useTranslation();
  const [keys, setKeys] = useState([]);
  const [loading, setLoading] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);
  const [editing, setEditing] = useState(null);
  const [newKey, setNewKey] = useState('');
  const [formApi, setFormApi] = useState(null);

  const fetchKeys = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/open_key/');
      if (res.data.success) setKeys(res.data.data || []);
    } catch (e) {
      showError(e.response?.data?.message || t('获取失败'));
    }
    setLoading(false);
  }, [t]);

  useEffect(() => {
    fetchKeys();
  }, [fetchKeys]);

  const openCreate = () => {
    setEditing(null);
    setNewKey('');
    setModalVisible(true);
  };

  const openEdit = (record) => {
    setEditing(record);
    setNewKey('');
    setModalVisible(true);
    let scope = {};
    try {
      scope = JSON.parse(record.scope || '{}');
    } catch (e) {}
    setTimeout(() => {
      formApi?.setValues({
        name: record.name,
        groups: (scope.groups || []).join(','),
        usernames: (scope.usernames || []).join(','),
        include_content: !!scope.include_content,
        max_days: scope.max_days || 30,
        enabled: record.enabled,
        expire_days: 0,
      });
    }, 50);
  };

  const submit = async () => {
    const values = formApi.getValues();
    if (!values.name?.trim()) {
      showError(t('请填写名称'));
      return;
    }
    const body = {
      id: editing?.id || 0,
      name: values.name.trim(),
      groups: (values.groups || '').split(',').map((s) => s.trim()).filter(Boolean),
      usernames: (values.usernames || '').split(',').map((s) => s.trim()).filter(Boolean),
      include_content: !!values.include_content,
      max_days: parseInt(values.max_days) || 30,
      enabled: values.enabled !== false,
      expire_days: parseInt(values.expire_days) || 0,
    };
    try {
      const res = editing
        ? await API.put('/api/open_key/', body)
        : await API.post('/api/open_key/', body);
      if (res.data.success) {
        showSuccess(editing ? t('已更新') : t('密钥已创建'));
        if (!editing && res.data.data?.key) {
          setNewKey(res.data.data.key);
        } else {
          setModalVisible(false);
        }
        fetchKeys();
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('操作失败'));
    }
  };

  const removeKey = async (id) => {
    try {
      const res = await API.delete(`/api/open_key/${id}`);
      if (res.data.success) {
        showSuccess(t('已删除'));
        fetchKeys();
      }
    } catch (e) {
      showError(e.response?.data?.message || t('操作失败'));
    }
  };

  const fmtTime = (ts) => {
    if (!ts) return '-';
    const d = new Date(ts * 1000);
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  };

  const scopeSummary = (record) => {
    let scope = {};
    try {
      scope = JSON.parse(record.scope || '{}');
    } catch (e) {}
    const parts = [];
    if (scope.groups?.length) parts.push(t('分组') + ': ' + scope.groups.join(','));
    if (scope.usernames?.length) parts.push(t('用户') + ': ' + scope.usernames.join(','));
    if (!parts.length) parts.push(t('全部用户'));
    parts.push(t('回看') + ' ' + (scope.max_days || 30) + t('天'));
    parts.push(scope.include_content ? t('含对话内容') : t('仅元数据'));
    return parts.join(' · ');
  };

  const columns = [
    { title: t('名称'), dataIndex: 'name', width: 130, render: (v) => <Text strong>{v}</Text> },
    {
      title: t('密钥'),
      dataIndex: 'key',
      width: 180,
      render: (v) => <Text code copyable>{v}</Text>,
    },
    { title: t('数据范围'), dataIndex: 'scope', render: (_, r) => <Text size='small'>{scopeSummary(r)}</Text> },
    {
      title: t('状态'),
      dataIndex: 'enabled',
      width: 80,
      render: (v, r) =>
        v && (r.expires_at === 0 || r.expires_at * 1000 > Date.now()) ? (
          <Tag color='green' size='small'>{t('启用')}</Tag>
        ) : (
          <Tag color='grey' size='small'>{t('停用')}</Tag>
        ),
    },
    { title: t('调用次数'), dataIndex: 'req_count', width: 90 },
    { title: t('最近使用'), dataIndex: 'last_used_at', width: 100, render: fmtTime },
    {
      title: '',
      dataIndex: 'op',
      width: 130,
      render: (_, r) => (
        <Space>
          <Button size='small' theme='light' onClick={() => openEdit(r)}>{t('编辑')}</Button>
          <Popconfirm title={t('确定删除该密钥？')} onConfirm={() => removeKey(r.id)}>
            <Button size='small' type='danger' theme='light'>{t('删除')}</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginBottom: 8,
          flexWrap: 'wrap',
          gap: 8,
        }}
      >
        <Text type='tertiary' size='small'>
          {t('发放给顾问等外部系统的只读密钥：按分组/用户圈定数据范围，可限制是否包含对话内容与回看天数')}
        </Text>
        <Button size='small' theme='solid' onClick={openCreate}>
          {t('新建密钥')}
        </Button>
      </div>
      <Table size='small' columns={columns} dataSource={keys} rowKey='id' loading={loading} pagination={false} />
      <Paragraph type='tertiary' size='small' style={{ marginTop: 8 }}>
        {t('调用方式（供顾问方）：')}
        <br />
        <Text code>
          {`GET {站点}/api/open/chat_logs?start=2026-01-01&end=2026-01-31&stats=1`}
        </Text>
        <br />
        <Text code>{`Authorization: Bearer sk-open-xxxx`}</Text>
        <br />
        {t('stats=1 返回按用户汇总（次数/token）；默认返回明细分页（page/page_size，单页≤100）；username/model_name 可过滤；时间跨度受密钥回看天数限制')}
      </Paragraph>

      <Modal
        title={editing ? t('编辑开放密钥') : t('新建开放密钥')}
        visible={modalVisible}
        onOk={submit}
        onCancel={() => setModalVisible(false)}
        okText={t('保存')}
        cancelText={t('取消')}
        width={520}
      >
        {newKey && (
          <Paragraph type='warning' style={{ marginBottom: 12 }}>
            {t('密钥已创建（仅此一次完整显示，请复制发给使用方）：')}
            <br />
            <Text code copyable>{newKey}</Text>
          </Paragraph>
        )}
        <Form getFormApi={(api) => setFormApi(api)}>
          <Form.Input field='name' label={t('名称')} placeholder={t('如：顾问团队-张三')} />
          <Form.Input
            field='groups'
            label={t('限定分组（逗号分隔，留空不限）')}
            placeholder={t('如：default,vip')}
          />
          <Form.Input
            field='usernames'
            label={t('限定用户（逗号分隔，与分组取并集）')}
            placeholder={t('如：user1,user2')}
          />
          <Form.Switch
            field='include_content'
            label={t('包含对话内容')}
            extraText={t('关闭时仅提供用户/模型/时间/token 等元数据，足以做使用总结')}
          />
          <Form.InputNumber
            field='max_days'
            label={t('最多回看天数')}
            extraText={t('1-366，默认 30')}
            min={1}
            max={366}
            initValue={30}
          />
          <Form.InputNumber
            field='expire_days'
            label={t('有效期（天，0=永久）')}
            min={0}
            initValue={0}
          />
          {editing && <Form.Switch field='enabled' label={t('启用')} initValue={true} />}
        </Form>
      </Modal>
    </div>
  );
};

export default OpenKeyPanel;
