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

import React from 'react';
import { Tag, Tooltip } from '@douyinfe/semi-ui';

// 把 token 数格式化为 K/M（如 262144 -> 256K）
export const formatTokenCount = (n) => {
  if (!n || n <= 0) return '-';
  if (n >= 1000000) {
    const v = n / 1000000;
    return `${Number.isInteger(v) ? v : v.toFixed(1)}M`;
  }
  if (n >= 1000) {
    const v = n / 1000;
    return `${Number.isInteger(v) ? v : v.toFixed(1)}K`;
  }
  return String(n);
};

// 可用状态标签：available=false 表示渠道全部被禁用（暂不可用）
export const renderAvailabilityTag = (record, t) => {
  if (record.available === false) {
    return (
      <Tag color='grey' shape='circle' size='small'>
        {t('暂不可用')}
      </Tag>
    );
  }
  return (
    <Tag color='green' shape='circle' size='small'>
      {t('可用')}
    </Tag>
  );
};

// 能力标签：视觉 / 推理 / 工具调用（只渲染支持的）
export const renderCapabilityTags = (record, t) => {
  const caps = [];
  if (record.supports_images) {
    caps.push(
      <Tag key='vision' color='blue' shape='circle' size='small'>
        {t('视觉')}
      </Tag>,
    );
  }
  if (record.supports_reasoning) {
    caps.push(
      <Tag key='reasoning' color='purple' shape='circle' size='small'>
        {t('推理')}
      </Tag>,
    );
  }
  if (record.supports_tool_call) {
    caps.push(
      <Tag key='tools' color='orange' shape='circle' size='small'>
        {t('工具调用')}
      </Tag>,
    );
  }
  if (caps.length === 0) return null;
  return <div className='flex items-center gap-1 flex-wrap'>{caps}</div>;
};

// 上下文窗口展示：maxInput / maxOutput
export const renderContextWindow = (record, t) => {
  const inStr = formatTokenCount(record.max_input_tokens);
  const outStr = formatTokenCount(record.max_output_tokens);
  if (inStr === '-' && outStr === '-') return null;
  return (
    <Tooltip content={`${t('最大输入')} / ${t('最大输出')} tokens`}>
      <span className='text-xs text-gray-500'>
        {inStr} / {outStr}
      </span>
    </Tooltip>
  );
};
