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
  Spin,
  Tag,
  Input,
  Popconfirm,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconDownload } from '@douyinfe/semi-icons';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';
import { useTranslation } from 'react-i18next';

const { Text, Title } = Typography;

// 影子代码库：从对话流抽取的文件按 用户/项目 物化为 git 仓库
const ShadowRepo = () => {
  const { t } = useTranslation();
  const [repos, setRepos] = useState([]);
  const [loading, setLoading] = useState(false);
  const [treeVisible, setTreeVisible] = useState(false);
  const [treeFiles, setTreeFiles] = useState([]);
  const [treeLoading, setTreeLoading] = useState(false);
  const [current, setCurrent] = useState(null);
  const [fileVisible, setFileVisible] = useState(false);
  const [fileContent, setFileContent] = useState('');
  const [fileLoading, setFileLoading] = useState(false);
  const [filter, setFilter] = useState('');
  const [selectedUser, setSelectedUser] = useState(null); // 按人汇总：选中用户名
  const [shadowUsers, setShadowUsers] = useState([]);
  const [projectsVisible, setProjectsVisible] = useState(false);
  const [projectsUser, setProjectsUser] = useState(null); // {user_id, username}

  const fetchUsers = useCallback(async () => {
    try {
      const res = await API.get('/api/shadow/users');
      if (res.data.success) setShadowUsers(res.data.data || []);
    } catch (e) {
      /* 静默，repos 拉取会报错 */
    }
  }, []);

  const openProjects = (u) => {
    setProjectsUser(u);
    setProjectsVisible(true);
    setSelectedUser(u.username);
    fetchRepos();
  };

  const scanUser = async (u) => {
    try {
      const res = await API.post(`/api/shadow/scan?user_id=${u.user_id}`);
      if (res.data.success) {
        showSuccess(res.data.message || t('已请求扫描'));
        setTimeout(fetchUsers, 3000);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('触发失败'));
    }
  };

  const fetchRepos = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/shadow/repos');
      if (res.data.success) {
        setRepos(res.data.data || []);
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('获取失败'));
    }
    setLoading(false);
  }, [t]);

  useEffect(() => {
    fetchRepos();
    fetchUsers();
  }, [fetchRepos, fetchUsers]);

  const openTree = async (repo) => {
    setCurrent(repo);
    setTreeVisible(true);
    setTreeLoading(true);
    try {
      const res = await API.get(
        `/api/shadow/tree?user_id=${repo.user_id}&project=${encodeURIComponent(repo.project_name)}`,
      );
      if (res.data.success) {
        setTreeFiles(res.data.data || []);
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('获取失败'));
    }
    setTreeLoading(false);
  };

  const openFile = async (path) => {
    setFileVisible(true);
    setFileLoading(true);
    try {
      const res = await API.get(
        `/api/shadow/file?user_id=${current.user_id}&project=${encodeURIComponent(current.project_name)}&path=${encodeURIComponent(path)}`,
      );
      if (res.data.success) {
        setFileContent(res.data.data?.content || t('（无内容：仅记录了读取操作）'));
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('获取失败'));
    }
    setFileLoading(false);
  };

  const triggerSync = async () => {
    try {
      const res = await API.post('/api/shadow/sync');
      if (res.data.success) {
        showSuccess(res.data.message || t('已触发物化'));
        setTimeout(fetchRepos, 2000);
      }
    } catch (e) {
      showError(e.response?.data?.message || t('触发失败'));
    }
  };

  const fmtBytes = (v) => {
    if (!v) return '-';
    if (v > 1024 * 1024) return (v / 1024 / 1024).toFixed(1) + ' MB';
    if (v > 1024) return (v / 1024).toFixed(1) + ' KB';
    return v + ' B';
  };

  const filtered = repos.filter(
    (r) =>
      !filter ||
      r.username?.includes(filter) ||
      r.project_name?.toLowerCase().includes(filter.toLowerCase()),
  );

  // 按人汇总
  const users = React.useMemo(() => {
    const m = new Map();
    for (const r of filtered) {
      const u = m.get(r.username) || { username: r.username, projects: 0, files: 0, last: 0 };
      u.projects += 1;
      u.files += r.file_count || 0;
      u.last = Math.max(u.last, r.last_update || 0);
      m.set(r.username, u);
    }
    return Array.from(m.values()).sort((a, b) => b.last - a.last);
  }, [filtered]);

  const shownRepos = selectedUser
    ? filtered.filter((r) => r.username === selectedUser)
    : filtered;

  return (
    <div className='mt-[60px] px-4 py-2'>
      <Card>
        <div className='flex items-center justify-between mb-4'>
          <div>
            <Title heading={5} style={{ marginBottom: 0 }}>
              {t('影子代码库')}
            </Title>
            <Text type='tertiary' size='small'>
              {t('按用户汇总的项目文件沉淀 · 每用户 24 小时自动扫描，也可手动触发 · 共 ')}
              {shadowUsers.length}
              {t(' 位用户')}
            </Text>
          </div>
          <div className='flex gap-2'>
            <Input
              placeholder={t('搜索用户/项目')}
              value={filter}
              onChange={setFilter}
              showClear
              style={{ width: 180 }}
            />
            <Button icon={<IconRefresh />} loading={loading} onClick={fetchRepos}>
              {t('刷新')}
            </Button>
            <Button onClick={triggerSync}>{t('立即物化')}</Button>
            <Popconfirm
              title={t('确定清空全部影子数据？')}
              content={t('删除所有已捕获的项目文件与描述，磁盘仓库一并清除，将从下一次扫描重建')}
              onConfirm={async () => {
                try {
                  const res = await API.post('/api/shadow/reset');
                  if (res.data.success) {
                    showSuccess(res.data.message || t('已清空'));
                    fetchRepos();
                    fetchUsers();
                  }
                } catch (e) {
                  showError(e.response?.data?.message || t('操作失败'));
                }
              }}
            >
              <Button type='danger' theme='light'>
                {t('清理重置')}
              </Button>
            </Popconfirm>
          </div>
        </div>
        {/* 主视图：用户列表 */}
        <Table
          size='small'
          dataSource={shadowUsers.filter((u) => !filter || u.username?.includes(filter))}
          rowKey='user_id'
          pagination={false}
          loading={loading}
          columns={[
            {
              title: t('用户'),
              dataIndex: 'username',
              width: 140,
              render: (v, r) => (
                <Text link onClick={() => openProjects(r)} style={{ fontWeight: 600 }}>
                  {v}
                </Text>
              ),
            },
            { title: t('项目数'), dataIndex: 'projects', width: 90 },
            { title: t('文件数'), dataIndex: 'files', width: 90 },
            {
              title: t('最近活动'),
              dataIndex: 'last_update',
              width: 160,
              render: (v) => (v ? timestamp2string(v) : '-'),
            },
            {
              title: t('上次扫描'),
              dataIndex: 'last_scan_at',
              width: 160,
              render: (v) => (v ? timestamp2string(v) : t('未扫描')),
            },
            {
              title: '',
              width: 200,
              render: (_, r) => (
                <div style={{ display: 'flex', gap: 4 }}>
                  <Button size='small' onClick={() => openProjects(r)}>
                    {t('查看项目')}
                  </Button>
                  <Button size='small' theme='solid' type='warning' onClick={() => scanUser(r)}>
                    {t('扫描')}
                  </Button>
                </div>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        title={t('项目文件')} 
        visible={treeVisible}
        onCancel={() => setTreeVisible(false)}
        footer={null}
        width={720}
      >
        <Text type='tertiary' size='small' style={{ display: 'block', marginBottom: 8 }}>
          {current?.username} / {current?.project_name} · {treeFiles.length} {t('个文件')}
        </Text>
        {treeLoading ? (
          <Spin />
        ) : (
          <Table
            size='small'
            dataSource={treeFiles}
            rowKey='file_path'
            pagination={treeFiles.length > 20 ? { pageSize: 20 } : false}
            columns={[
              {
                title: t('文件'),
                dataIndex: 'file_path',
                render: (v) => (
                  <Text
                    link
                    style={{ wordBreak: 'break-all' }}
                    onClick={() => openFile(v)}
                  >
                    {v}
                  </Text>
                ),
              },
              {
                title: t('操作'),
                dataIndex: 'action',
                width: 80,
                render: (v) => (
                  <Tag color={v === 'write' ? 'green' : v === 'edit' ? 'orange' : 'grey'}>
                    {v}
                  </Tag>
                ),
              },
              {
                title: t('大小'),
                dataIndex: 'content_len',
                width: 90,
                render: (v) => fmtBytes(v),
              },
              {
                title: t('更新时间'),
                dataIndex: 'updated_at',
                width: 150,
                render: (v) => (v ? timestamp2string(v) : '-'),
              },
            ]}
          />
        )}
      </Modal>

      <Modal
        title={t('文件内容')}
        visible={fileVisible}
        onCancel={() => setFileVisible(false)}
        footer={null}
        width={860}
      >
        {fileLoading ? (
          <Spin />
        ) : (
          <pre
            style={{
              maxHeight: 480,
              overflow: 'auto',
              fontSize: 12,
              lineHeight: 1.5,
              padding: 12,
              background: 'var(--semi-color-fill-0)',
              borderRadius: 8,
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-all',
            }}
          >
            {fileContent}
          </pre>
        )}
      </Modal>

      {/* 用户项目列表 */}
      <Modal
        title={`${projectsUser?.username || ''} · ${t('项目')}`}
        visible={projectsVisible}
        onCancel={() => {
          setProjectsVisible(false);
          setSelectedUser(null);
        }}
        footer={null}
        width={860}
      >
        <Table
          size='small'
          dataSource={filtered.filter((r) => r.username === projectsUser?.username)}
          rowKey={(r) => r.user_id + '/' + r.project_name}
          pagination={false}
          columns={[
            {
              title: t('项目'),
              dataIndex: 'project_name',
              render: (v) => <Tag color='blue'>{v}</Tag>,
            },
            {
              title: t('功能描述'),
              dataIndex: 'description',
              render: (v) =>
                v ? (
                  <Text style={{ maxWidth: 260 }} ellipsis={{ showTooltip: true }}>
                    {v}
                  </Text>
                ) : (
                  <Text type='tertiary'>{t('生成中…')}</Text>
                ),
            },
            { title: t('文件数'), dataIndex: 'file_count', width: 80 },
            {
              title: t('最近更新'),
              dataIndex: 'last_update',
              width: 150,
              render: (v) => (v ? timestamp2string(v) : '-'),
            },
            {
              title: '',
              width: 190,
              render: (_, r) => (
                <div style={{ display: 'flex', gap: 4 }}>
                  <Button size='small' onClick={() => openTree(r)}>
                    {t('查看文件')}
                  </Button>
                  <Button
                    size='small'
                    icon={<IconDownload size={13} />}
                    onClick={() =>
                      window.open(
                        `/api/shadow/download?user_id=${r.user_id}&project=${encodeURIComponent(r.project_name)}`,
                        '_blank',
                      )
                    }
                  >
                    {t('zip')}
                  </Button>
                </div>
              ),
            },
          ]}
        />
      </Modal>
    </div>
  );
};

export default ShadowRepo;
