import React, { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Card,
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

  // 生成一份可转发的接入文档（含三个接口示例与审查提示词）
  const buildDoc = () => {
    const base = window.location.origin;
    return [
      t('数据开放接口使用说明'),
      '',
      t('鉴权：请求头 Authorization: Bearer <开放密钥>'),
      '',
      t('1) 模型调用情况'),
      `GET ${base}/api/open/usage?start=2026-09-01&end=2026-09-30&group_by=user_model`,
      t('参数：start/end（YYYY-MM-DD，默认最近 7 天）、users（逗号分隔）、model、group、group_by=user|model|user_model、limit、format=json|text|csv'),
      t('返回：每个用户/模型的调用次数、输入输出 token、费用（元）、首次与最后调用时间'),
      '',
      t('2) 调用内容（提问与回复原文）'),
      `GET ${base}/api/open/contents?users=张三&start=2026-09-01&end=2026-09-30&limit=50&max_chars=2000`,
      t('参数：max_chars 控制每条内容截断长度（0=不截断）、format=json|text'),
      '',
      t('3) 一站式报告（推荐：一次调用拿到用量+明细+内容样本）'),
      `GET ${base}/api/open/report?start=2026-09-01&end=2026-09-30&top_users=20&samples=3&max_chars=1200&format=text`,
      t('返回按费用降序的用户汇总、各模型明细、每人最近若干条内容样本，并附审查提示词，可直接连同输出发给 AI'),
      '',
      t('说明：数据范围由密钥限定（分组/用户/回看天数/是否含内容）；users 只能填授权范围内的用户'),
    ].join('\n');
  };

  const copyDoc = async () => {
    try {
      await navigator.clipboard.writeText(buildDoc());
      showSuccess(t('接入说明已复制，可直接发给顾问'));
    } catch (e) {
      showError(t('复制失败，请手动选择文本复制'));
    }
  };

  // 复制「给 AI 智能体的使用说明」：用该密钥实时拉取 /api/open/guide，
  // 文档里已注入这条密钥的真实数据范围（能查哪些人、有没有内容权限、回看天数），
  // 顾问把它连同说明一起发给智能体即可自助开工。
  const copyAgentGuide = async (key) => {
    try {
      const res = await fetch('/api/open/guide', {
        headers: { Authorization: 'Bearer ' + key },
      });
      const text = await res.text();
      // 说明本身是 markdown，只有「接口报错」时才是 JSON 体；不能简单地在全文里搜
      // "success":false ——文档的错误处理章节里就写着这个串，会误判（实测踩过）。
      const looksLikeErrorJson = text.trimStart().startsWith('{');
      if (!res.ok || looksLikeErrorJson) {
        showError(t('获取说明失败，请检查密钥是否有效'));
        return;
      }
      await navigator.clipboard.writeText(text);
      showSuccess(t('智能体说明已复制（已按该密钥的数据范围生成）'));
    } catch (e) {
      showError(t('复制失败，请手动选择文本复制'));
    }
  };

  const openCreate = () => {
    setEditing(null);
    setNewKey('');
    setModalVisible(true);
  };

  // 编辑时用 key 重挂载 Form + initValues 初始化，不依赖 formApi 回填时序
  const buildInitValues = (record) => {
    let scope = {};
    try {
      scope = JSON.parse(record.scope || '{}');
    } catch (e) {}
    return {
      name: record.name,
      groups: (scope.groups || []).join(','),
      usernames: (scope.usernames || []).join(','),
      include_content: !!scope.include_content,
      max_days: scope.max_days || 30,
      enabled: record.enabled,
      expire_days: 0,
    };
  };

  const openEdit = (record) => {
    setEditing(record);
    setNewKey('');
    setModalVisible(true);
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
      width: 250,
      render: (_, r) => (
        <Space>
          <Button
            size='small'
            theme='light'
            onClick={() => copyAgentGuide(r.key)}
          >
            {t('复制智能体说明')}
          </Button>
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
      <Card
        className='!rounded-2xl shadow-sm border-0'
        style={{ marginTop: 8 }}
        bodyStyle={{ padding: 12 }}
      >
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            gap: 8,
            flexWrap: 'wrap',
          }}
        >
          <Text strong>{t('顾问接入说明（含示例与审查提示词）')}</Text>
          <Space>
            <Button size='small' theme='light' onClick={copyDoc}>
              {t('复制完整说明')}
            </Button>
          </Space>
        </div>
        <Paragraph type='tertiary' size='small' style={{ marginTop: 8, marginBottom: 0 }}>
          {t('所有接口都用同一个请求头鉴权：')}
          <Text code>{`Authorization: Bearer sk-open-xxxx`}</Text>
          <br />
          {t('① 模型调用情况（次数/token/费用，可按用户、模型、用户×模型汇总）：')}
          <Text code>{`GET {站点}/api/open/usage?start=2026-09-01&end=2026-09-30&group_by=user_model`}</Text>
          <br />
          {t('② 调用内容（提问与回复原文，max_chars 截断便于喂给 AI）：')}
          <Text code>{`GET {站点}/api/open/contents?users=张三,李四&limit=50&max_chars=2000`}</Text>
          <br />
          {t('③ 一站式报告（用量 + 模型明细 + 内容样本 + 现成审查提示词，format=text 直接发给 AI）：')}
          <Text code>{`GET {站点}/api/open/report?start=2026-09-01&end=2026-09-30&top_users=20&samples=3&format=text`}</Text>
          <br />
          {t('通用参数：start / end（支持 YYYY-MM-DD，默认最近 7 天）、users（逗号分隔，缺省=授权范围内全部）、model、group、format=json|text|csv、limit。时间跨度受密钥「最多回看天数」限制；需要内容时密钥必须勾选「包含对话内容」')}
          <br />
          {t('④ 用户额度概览（总额度/已用/剩余 + 累计调用次数）：')}
          <Text code>{`GET {站点}/api/open/users?format=json`}</Text>
          <br />
          {t('智能体自助说明：GET /api/open/guide（markdown，已注入该密钥的数据范围；OpenAPI 描述见 /api/open/openapi.json）。密钥列表里每行都有「复制智能体说明」，复制出来可直接发给 AI 智能体')}
          <br />
          {t('原有 GET /api/open/chat_logs 保留不变（stats=1 汇总、format=csv 报表、默认明细分页）')}
        </Paragraph>
      </Card>

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
        <Form
          key={editing ? 'edit-' + editing.id : 'create'}
          initValues={
            editing ? buildInitValues(editing) : { max_days: 30, expire_days: 0, enabled: true }
          }
          getFormApi={(api) => setFormApi(api)}
        >
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
          />
          <Form.InputNumber
            field='expire_days'
            label={t('有效期（天，0=保持不变）')}
            extraText={t('新建时 0=永久；编辑时 0=保持当前有效期，输入正数则从现在重新计算')}
            min={0}
          />
          {editing && <Form.Switch field='enabled' label={t('启用')} initValue={true} />}
        </Form>
      </Modal>
    </div>
  );
};

export default OpenKeyPanel;
