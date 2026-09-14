/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useCallback, useEffect, useState } from 'react';
import {
  Card,
  Table,
  Typography,
  Button,
  Modal,
  Form,
  Select,
  Input,
  Tag,
  Progress,
  Space,
  Popconfirm,
  Banner,
  Spin,
} from '@douyinfe/semi-ui';
import {
  IconRefresh,
  IconPlus,
  IconDelete,
  IconEdit,
} from '@douyinfe/semi-icons';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';
import { useTranslation } from 'react-i18next';

const { Text, Title } = Typography;

// 套餐额度监控页
const QuotaMonitor = () => {
  const { t } = useTranslation();
  const [accounts, setAccounts] = useState([]);
  const [loading, setLoading] = useState(false);
  const [editVisible, setEditVisible] = useState(false);
  const [editing, setEditing] = useState(null);
  const [formApi, setFormApi] = useState(null);
  const [browserVisible, setBrowserVisible] = useState(false);
  const [browserSession, setBrowserSession] = useState(null);
  const [browserStep, setBrowserStep] = useState('init');
  const [browserLoading, setBrowserLoading] = useState(false);
  const [shotTick, setShotTick] = useState(0);
  const [loginFormApi, setLoginFormApi] = useState(null);

  const fetchAccounts = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/quota/accounts');
      if (res.data.success) {
        setAccounts(res.data.data || []);
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('获取失败'));
    }
    setLoading(false);
  }, [t]);

  useEffect(() => {
    fetchAccounts();
  }, [fetchAccounts]);

  const refreshAll = async () => {
    try {
      const res = await API.post('/api/quota/refresh');
      showSuccess(res.data.message || t('已触发刷新'));
      setTimeout(fetchAccounts, 5000);
    } catch (e) {
      showError(e.response?.data?.message || t('刷新失败'));
    }
  };

  const refreshOne = async (id) => {
    try {
      const res = await API.post(`/api/quota/refresh?id=${id}`);
      if (res.data.success) {
        showSuccess(t('刷新完成'));
        fetchAccounts();
      } else {
        showError(res.data.message || t('刷新失败'));
      }
    } catch (e) {
      showError(e.response?.data?.message || t('刷新失败'));
    }
  };

  const openEdit = (account) => {
    setEditing(account);
    setEditVisible(true);
  };

  const handleSave = async () => {
    const values = formApi?.getValues() || {};
    try {
      const body = {
        provider: values.provider || 'zhipu',
        account_name: values.account_name,
        session_token: values.session_token,
        org_id: values.org_id || '',
      };
      let res;
      if (editing) {
        body.id = editing.id;
        res = await API.put('/api/quota/accounts', body);
      } else {
        res = await API.post('/api/quota/accounts', body);
      }
      if (res.data.success) {
        showSuccess(editing ? t('已更新') : t('已添加'));
        setEditVisible(false);
        fetchAccounts();
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('保存失败'));
    }
  };

  const handleDelete = async (id) => {
    try {
      await API.delete(`/api/quota/accounts?id=${id}`);
      showSuccess(t('已删除'));
      fetchAccounts();
    } catch (e) {
      showError(e.response?.data?.message || t('删除失败'));
    }
  };

  const parseQuota = (account) => {
    if (!account.quota_data) return null;
    try {
      return JSON.parse(account.quota_data);
    } catch {
      return null;
    }
  };

  const fmtTokens = (n) => {
    if (!n) return '-';
    if (n > 1e9) return (n / 1e9).toFixed(1) + 'B';
    if (n > 1e6) return (n / 1e6).toFixed(1) + 'M';
    if (n > 1e3) return (n / 1e3).toFixed(1) + 'K';
    return String(n);
  };

  const statusTag = (a) => {
    if (!a.session_token) return <Tag color='grey'>{t('未设置Token')}</Tag>;
    if (a.fetch_status === 'ok') return <Tag color='green'>{t('正常')}</Tag>;
    if (a.fetch_status === 'expired')
      return <Tag color='red'>{t('Token过期')}</Tag>;
    if (a.fetch_status === 'error')
      return <Tag color='orange'>{t('异常')}</Tag>;
    return <Tag color='blue'>{t('待抓取')}</Tag>;
  };

  return (
    <div className='mt-[60px] px-4 py-2'>
      <Card>
        <div className='flex items-center justify-between mb-4'>
          <div>
            <Title heading={5} style={{ marginBottom: 0 }}>
              {t('套餐额度监控')}
            </Title>
            <Text type='tertiary' size='small'>
              {t('多云服务商 Coding Plan 用量追踪 · 30 分钟自动刷新 · 共 ')}
              {accounts.length}
              {t(' 个账号')}
            </Text>
          </div>
          <Space>
            <Button icon={<IconPlus />} onClick={() => openEdit(null)}>
              {t('添加账号')}
            </Button>
            <Button icon={<IconRefresh />} loading={loading} onClick={refreshAll}>
              {t('全部刷新')}
            </Button>
          </Space>
        </div>

        <Banner
          type='info'
          description={t(
            '智谱有验证码防护无法自动登录。Token 获取：浏览器登录 bigmodel.cn → F12 → Network → 刷新 coding-plan 页面 → 任一请求的 Request Headers 里复制 Authorization 值。Token 过期后重新导入。',
          )}
          closeIcon={null}
          style={{ marginBottom: 12 }}
        />

        <Table
          size='small'
          dataSource={accounts}
          rowKey='id'
          pagination={false}
          loading={loading}
          columns={[
            {
              title: t('服务商'),
              dataIndex: 'provider',
              width: 80,
              render: (v) => <Tag color={v === 'zhipu' ? 'blue' : 'cyan'}>{v}</Tag>,
            },
            {
              title: t('账号'),
              dataIndex: 'account_name',
              width: 150,
            },
            { title: t('状态'), width: 90, render: (_, a) => statusTag(a) },
            {
              title: t('套餐用量'),
              render: (_, a) => {
                const q = parseQuota(a);
                if (!q) return <Text type='tertiary'>-</Text>;
                const pct = q.used_percent || 0;
                const color = pct > 90 ? 'red' : pct > 70 ? 'orange' : 'green';
                return (
                  <div style={{ minWidth: 200 }}>
                    <div>
                      <Text size='small'>
                        {fmtTokens(q.used_tokens)} / {fmtTokens(q.total_tokens)}{' '}
                        ({pct.toFixed(1)}%)
                      </Text>
                    </div>
                    <Progress percent={pct} stroke={color} showInfo={false} />
                    {q.plan_name && (
                      <Text type='tertiary' size='small'>
                        {q.plan_name}
                        {q.expire_at ? ` · ${t('到期')}: ${q.expire_at}` : ''}
                      </Text>
                    )}
                  </div>
                );
              },
            },
            {
              title: t('最近抓取'),
              dataIndex: 'last_fetch_at',
              width: 150,
              render: (v) => (v ? timestamp2string(v) : '-'),
            },
            {
              title: '',
              width: 260,
              render: (_, a) => (
                <Space>
                  <Button size='small' onClick={() => refreshOne(a.id)}>
                    {t('刷新')}
                  </Button>
                  <Button size='small' theme='solid' type='primary' onClick={() => { setEditing(a); setBrowserVisible(true); setBrowserStep('init'); }}>{t('连接')}</Button>
                  <Button size='small' icon={<IconEdit size={13} />} onClick={() => openEdit(a)}>
                    {t('编辑')}
                  </Button>
                  <Popconfirm title={t('确定删除？')} onConfirm={() => handleDelete(a.id)}>
                    <Button size='small' type='danger' theme='light' icon={<IconDelete size={13} />} />
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        title={editing ? t('编辑账号') : t('添加账号')}
        visible={editVisible}
        onCancel={() => setEditVisible(false)}
        onOk={handleSave}
        width={520}
      >
        <Form getFormApi={(api) => setFormApi(api)} key={editing?.id || 'new'}>
          <Form.Select
            field='provider'
            label={t('服务商')}
            initValue={editing?.provider || 'zhipu'}
            optionList={[
              { value: 'zhipu', label: '智谱 (bigmodel.cn)' },
            ]}
            disabled={!!editing}
          />
          <Form.Input
            field='account_name'
            label={t('账号名')}
            initValue={editing?.account_name}
            placeholder={t('手机号/邮箱（显示用）')}
          />
          <Form.TextArea
            field='session_token'
            label={t('Session Token')}
            initValue={''}
            placeholder={t(
              '浏览器 F12 → Network → coding-plan 页面任一请求的 Authorization 值',
            )}
            rows={3}
          />
          <Form.Input
            field='org_id'
            label={t('机构 ID（多机构账号需填）')}
            initValue={editing?.org_id}
            placeholder={t('留空则用默认机构')}
          />
        </Form>
      </Modal>

      {/* Browser Login Modal */}
      <Modal
        title={t('智谱账号连接')}
        visible={browserVisible}
        onCancel={() => {
          if (browserSession) API.post('/api/quota/browser/close', { session: browserSession }).catch(() => {});
          setBrowserVisible(false); setBrowserSession(null); setBrowserStep('init');
        }}
        footer={null} width={860} bodyStyle={{ maxHeight: '75vh', overflow: 'auto' }}
      >
        {browserStep === 'init' && (
          <div style={{ textAlign: 'center', padding: '40px 0' }}>
            <Text style={{ display: 'block', marginBottom: 16 }}>{t('启动浏览器登录智谱账号，全程无需离开本页')}</Text>
            <Button theme='solid' type='primary' loading={browserLoading} onClick={async () => {
              setBrowserLoading(true);
              try {
                const res = await API.post('/api/quota/browser/start');
                if (res.data.success) { setBrowserSession(res.data.data.session_id); setBrowserStep('screenshot'); setShotTick(Date.now()); }
                else showError(res.data.message || t('启动失败'));
              } catch (e) { showError(e.response?.data?.message || t('启动失败')); }
              setBrowserLoading(false);
            }}>{t('启动浏览器')}</Button>
          </div>
        )}
        {browserStep === 'screenshot' && browserSession && (
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
              <Button size='small' icon={<IconRefresh size={14} />} onClick={() => setShotTick(Date.now())}>{t('刷新截图')}</Button>
              <Text type='tertiary' size='small'>{t('下方是智谱登录页实时截图')}</Text>
            </div>
            <div style={{ border: '1px solid var(--semi-color-border)', borderRadius: 8, overflow: 'hidden', marginBottom: 12 }}>
              <img src={'/api/quota/browser/screenshot?session=' + browserSession + '&t=' + shotTick}
                   style={{ width: '100%' }} alt={t('截图')} />
            </div>
            <Form getFormApi={(api) => setLoginFormApi(api)} key='browser-login'>
              <Form.Input field='phone' label={t('手机号/邮箱')} placeholder={t('智谱登录账号')} />
              <Form.Input field='password' label={t('密码')} mode='password' placeholder={t('智谱登录密码')} />
            </Form>
            <div style={{ display: 'flex', gap: 8, justifyContent: 'center', marginTop: 12 }}>
              <Button theme='solid' type='primary' loading={browserLoading} onClick={async () => {
                const v = loginFormApi?.getValues() || {};
                if (!v.phone || !v.password) { showError(t('请输入账号和密码')); return; }
                setBrowserLoading(true);
                try {
                  await API.post('/api/quota/browser/fill', { session: browserSession, phone: v.phone, password: v.password });
                  await API.post('/api/quota/browser/submit', { session: browserSession });
                  setBrowserStep('capturing');
                } catch (e) { showError(e.response?.data?.message || t('操作失败')); }
                setBrowserLoading(false);
              }}>{t('填入并登录')}</Button>
            </div>
          </div>
        )}
        {browserStep === 'capturing' && browserSession && (
          <div style={{ textAlign: 'center', padding: '40px 0' }}>
            <Spin size='large' />
            <Text style={{ display: 'block', marginTop: 12 }}>{t('正在等待登录完成...')}</Text>
            <Text type='tertiary' size='small' style={{ display: 'block', marginTop: 4 }}>{t('如有验证码请点下方返回查看截图')}</Text>
            <Button size='small' style={{ marginTop: 8 }} onClick={() => { setBrowserStep('screenshot'); setShotTick(Date.now()); }}>{t('返回截图')}</Button>
            <TokenCapturePoller session={browserSession} accountId={editing?.id} onDone={() => { setBrowserStep('done'); fetchAccounts(); }} onFail={(m) => { showError(m); setBrowserStep('screenshot'); }} />
          </div>
        )}
        {browserStep === 'done' && (
          <div style={{ textAlign: 'center', padding: '40px 0' }}>
            <Tag color='green' size='large'>{t('登录成功')}</Tag>
            <Text style={{ display: 'block', marginTop: 12 }}>{t('Token 已保存，额度将自动刷新')}</Text>
            <Button theme='solid' type='primary' style={{ marginTop: 12 }} onClick={() => setBrowserVisible(false)}>{t('完成')}</Button>
          </div>
        )}
      </Modal>
    </div>
  );
};

const TokenCapturePoller = ({ session, accountId, onDone, onFail }) => {
  const started = React.useRef(false);
  React.useEffect(() => {
    if (!session || started.current) return;
    started.current = true;
    const poll = async () => {
      try {
        const res = await API.post('/api/quota/browser/capture', { session, account_id: accountId || undefined, timeout: 25 });
        if (res.data.success) onDone();
        else onFail(res.data.message || '未捕获到 Token');
      } catch (e) { onFail(e?.response?.data?.message || '捕获失败'); }
    };
    poll();
  }, [session]);
  return null;
};


export default QuotaMonitor;
