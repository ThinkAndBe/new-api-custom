/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useMemo } from 'react';
import { Modal, Table, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { renderQuota } from '../../../../helpers';

const { Text } = Typography;

// 按用户汇总弹窗：当前筛选条件（时间段/模型/用户名/分组/渠道）下的
// 每用户请求数与 token 用量汇总
const UserStatsModal = ({ visible, onClose, userStats = [] }) => {
  const { t } = useTranslation();

  const totals = useMemo(() => {
    return userStats.reduce(
      (acc, r) => ({
        count: acc.count + (r.count || 0),
        prompt: acc.prompt + (r.prompt_tokens || 0),
        completion: acc.completion + (r.completion_tokens || 0),
        quota: acc.quota + (r.quota || 0),
      }),
      { count: 0, prompt: 0, completion: 0, quota: 0 },
    );
  }, [userStats]);

  const fmt = (v) => (v != null ? Number(v).toLocaleString() : '-');

  const columns = [
    { title: t('用户名'), dataIndex: 'username', width: 140 },
    {
      title: t('对话次数'),
      dataIndex: 'count',
      width: 100,
      render: (v) => fmt(v),
    },
    {
      title: t('输入 Tokens'),
      dataIndex: 'prompt_tokens',
      render: (v) => fmt(v),
    },
    {
      title: t('输出 Tokens'),
      dataIndex: 'completion_tokens',
      render: (v) => fmt(v),
    },
    {
      title: t('总 Tokens'),
      render: (_, r) =>
        fmt((r.prompt_tokens || 0) + (r.completion_tokens || 0)),
    },
    {
      title: t('花费'),
      dataIndex: 'quota',
      width: 110,
      render: (v) => renderQuota(v || 0),
    },
  ];

  return (
    <Modal
      title={t('按用户汇总（当前筛选条件）')}
      visible={visible}
      onCancel={onClose}
      footer={null}
      width={760}
    >
      <Text type='tertiary' size='small' style={{ display: 'block', marginBottom: 8 }}>
        {t('共 {{users}} 个用户：{{count}} 次对话，输入 {{prompt}} / 输出 {{completion}} tokens，花费 {{quota}}', {
          users: userStats.length,
          count: fmt(totals.count),
          prompt: fmt(totals.prompt),
          completion: fmt(totals.completion),
          quota: renderQuota(totals.quota),
        })}
      </Text>
      <Table
        size='small'
        dataSource={userStats}
        rowKey='user_id'
        columns={columns}
        pagination={userStats.length > 20 ? { pageSize: 20 } : false}
      />
    </Modal>
  );
};

export default UserStatsModal;
