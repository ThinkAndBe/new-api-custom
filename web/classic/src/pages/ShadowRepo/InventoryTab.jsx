import React, { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Table,
  Tag,
  Button,
  Input,
  Radio,
  RadioGroup,
  Modal,
  TextArea,
  Typography,
  Space,
  Popconfirm,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../helpers';

const { Text } = Typography;

// 影子库清单模式：工具扫描上报的项目/技能清单 + 手动拉取 + 不关注
const InventoryTab = () => {
  const { t } = useTranslation();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(false);
  const [keyword, setKeyword] = useState('');
  const [kind, setKind] = useState('');
  const [view, setView] = useState('active'); // active | ignored
  const [editItem, setEditItem] = useState(null);
  const [editPurpose, setEditPurpose] = useState('');

  const fetchItems = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      if (keyword) params.set('keyword', keyword);
      if (kind) params.set('kind', kind);
      params.set('ignored', view === 'ignored' ? 'true' : 'false');
      const res = await API.get(`/api/shadow/inventory?${params.toString()}`);
      if (res.data.success) setItems(res.data.data || []);
      else showError(res.data.message);
    } catch (e) {
      showError(e.response?.data?.message || t('获取失败'));
    }
    setLoading(false);
  }, [keyword, kind, view, t]);

  useEffect(() => {
    fetchItems();
  }, [fetchItems]);

  const pullItem = async (record) => {
    try {
      const res = await API.post('/api/shadow/inventory/pull', { id: record.id });
      if (res.data.success) {
        showSuccess(t('拉取任务已下发，等待该用户工具在线执行'));
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('操作失败'));
    }
  };

  const setIgnored = async (record, ignored) => {
    try {
      const res = await API.post('/api/shadow/inventory/ignore', {
        id: record.id,
        ignored,
      });
      if (res.data.success) {
        showSuccess(ignored ? t('已标记为不关注') : t('已恢复关注'));
        fetchItems();
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('操作失败'));
    }
  };

  const savePurpose = async () => {
    try {
      const res = await API.put('/api/shadow/inventory/purpose', {
        id: editItem.id,
        purpose: editPurpose,
      });
      if (res.data.success) {
        showSuccess(t('用途已更新'));
        setEditItem(null);
        fetchItems();
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('操作失败'));
    }
  };

  const fmtTime = (ts) => {
    if (!ts) return '-';
    const d = new Date(ts * 1000);
    return `${d.getMonth() + 1}-${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
  };

  const columns = [
    {
      title: t('用户'),
      dataIndex: 'username',
      width: 110,
      render: (v) => <Text strong>{v}</Text>,
    },
    {
      title: t('类型'),
      dataIndex: 'kind',
      width: 70,
      render: (v) =>
        v === 'skill' ? (
          <Tag color='green' size='small'>
            {t('技能')}
          </Tag>
        ) : (
          <Tag color='blue' size='small'>
            {t('项目')}
          </Tag>
        ),
    },
    {
      title: t('名称'),
      dataIndex: 'name',
      width: 180,
      render: (v, r) => (
        <div>
          <Text strong>{v}</Text>
          <Text type='tertiary' size='small' style={{ display: 'block' }}>
            {r.tool}
          </Text>
        </div>
      ),
    },
    {
      title: t('用途'),
      dataIndex: 'purpose',
      ellipsis: true,
      render: (v) => (
        <Text type='secondary' size='small'>
          {v || t('（待补充，可点编辑填写）')}
        </Text>
      ),
    },
    { title: t('文件数'), dataIndex: 'file_count', width: 80 },
    {
      title: t('最近扫描'),
      dataIndex: 'last_scan_at',
      width: 110,
      render: fmtTime,
    },
    {
      title: '',
      dataIndex: 'operate',
      width: 190,
      render: (_, record) => (
        <Space>
          {view === 'active' ? (
            <>
              <Button size='small' theme='solid' onClick={() => pullItem(record)}>
                {t('拉取')}
              </Button>
              <Popconfirm
                title={t('标记后移入不关注列表，可随时恢复')}
                onConfirm={() => setIgnored(record, true)}
              >
                <Button size='small' theme='light'>
                  {t('不关注')}
                </Button>
              </Popconfirm>
            </>
          ) : (
            <Button size='small' theme='light' onClick={() => setIgnored(record, false)}>
              {t('恢复关注')}
            </Button>
          )}
          <Button
            size='small'
            theme='light'
            onClick={() => {
              setEditItem(record);
              setEditPurpose(record.purpose || '');
            }}
          >
            {t('编辑用途')}
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div
        style={{
          display: 'flex',
          gap: 12,
          marginBottom: 12,
          flexWrap: 'wrap',
          alignItems: 'center',
        }}
      >
        <RadioGroup
          type='button'
          value={view}
          onChange={(e) => setView(e.target.value)}
        >
          <Radio value='active'>{t('全部清单')}</Radio>
          <Radio value='ignored'>{t('不关注')}</Radio>
        </RadioGroup>
        <RadioGroup type='button' value={kind} onChange={(e) => setKind(e.target.value)}>
          <Radio value=''>{t('全部')}</Radio>
          <Radio value='project'>{t('项目')}</Radio>
          <Radio value='skill'>{t('技能')}</Radio>
        </RadioGroup>
        <Input
          style={{ width: 220 }}
          placeholder={t('搜索名称/用户/用途')}
          value={keyword}
          onChange={setKeyword}
          showClear
        />
        <Text type='tertiary' size='small'>
          {t('清单来自各用户常驻工具扫描上报；点「拉取」后等其工具在线时自动上传文件')}
        </Text>
      </div>
      <Table
        columns={columns}
        dataSource={items}
        loading={loading}
        rowKey='id'
        pagination={{ pageSize: 20 }}
        size='small'
      />
      <Modal
        title={t('编辑用途') + ' · ' + (editItem?.name || '')}
        visible={!!editItem}
        onOk={savePurpose}
        onCancel={() => setEditItem(null)}
        okText={t('保存')}
        cancelText={t('取消')}
      >
        <TextArea
          rows={4}
          value={editPurpose}
          onChange={setEditPurpose}
          placeholder={t('填写该项目/技能的用途说明')}
        />
      </Modal>
    </div>
  );
};

export default InventoryTab;
