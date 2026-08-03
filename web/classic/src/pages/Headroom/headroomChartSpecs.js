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

import { renderNumber } from '../../helpers';

/**
 * 生成压缩看板排行图（横向柱状图）的 VChart spec
 */
export const makeRankSpec = (title, data, t) => ({
  type: 'bar',
  direction: 'horizontal',
  data: [{ id: 'data', values: (data || []).slice(0, 15) }],
  xField: 'tokens_saved',
  yField: 'name',
  seriesField: 'name',
  legends: { visible: false },
  title: { visible: true, text: title },
    label: {
      visible: true,
      position: 'outside',
      formatMethod: (v) => renderNumber(v || 0),
    },
    tooltip: {
      mark: {
        content: [
          {
            key: (d) => d.name,
            value: (d) => `${renderNumber(d.tokens_saved)} tokens`,
          },
          {
            key: () => t('请求数'),
            value: (d) => renderNumber(d.request_count || 0),
          },
        ],
      },
    },
  });

/**
 * 生成压缩看板历史趋势图（堆叠面积图）的 VChart spec
 * trendData 已在组件中预展平，这里不再做 flatMap，避免每次渲染新建 spec 时重复计算。
 */
export const makeTrendSpec = (trendData, granularity, t) => {
  const granularityLabel =
    granularity === 'month'
      ? t('按月')
      : granularity === 'week'
        ? t('按周')
        : t('按日');
  return {
    type: 'area',
    stack: true,
    data: [{ id: 'trendData', values: trendData || [] }],
    xField: 'time',
    yField: 'value',
    seriesField: 'type',
    legends: { visible: true, selectMode: 'single' },
    title: { visible: true, text: `${t('历史趋势')}（${granularityLabel}）` },
    axes: [
      { orient: 'left', label: { formatMethod: (v) => renderNumber(v || 0) } },
      { orient: 'bottom', label: { autoLimit: false, autoHide: true } },
    ],
    area: { style: { fillOpacity: 0.25 } },
    line: { style: { lineWidth: 2 } },
    point: { visible: false },
    tooltip: {
      mark: {
        content: [
          {
            key: (d) => d.type,
            value: (d) => `${renderNumber(d.value)} tokens`,
          },
          {
            key: () => t('请求数'),
            value: (d) => renderNumber(d.request_count || 0),
          },
          {
            key: () => t('节省率'),
            value: (d) => `${((d.average_ratio || 0) * 100).toFixed(1)}%`,
          },
        ],
      },
    },
  };
};
