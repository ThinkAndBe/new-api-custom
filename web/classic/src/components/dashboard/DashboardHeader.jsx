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
import { Button, Toast } from '@douyinfe/semi-ui';
import { RefreshCw, Search, Download, FileJson } from 'lucide-react';
import { downloadCSV, genExportFilename, timestamp2string } from '../../helpers';

const DashboardHeader = ({
  getGreeting,
  greetingVisible,
  showSearchModal,
  refresh,
  loading,
  quotaData,
  inputs,
  onDownloadConfig,
  t,
}) => {
  const ICON_BUTTON_CLASS = 'text-white hover:bg-opacity-80 !rounded-full';

  // 将当前看板的 quotaData（按模型/时间聚合）导出为 CSV
  const handleExport = () => {
    if (!quotaData || quotaData.length === 0) {
      Toast.warning(t('当前无数据可导出'));
      return;
    }
    const rows = [
      [
        t('时间'),
        t('模型'),
        t('调用次数'),
        t('Token 用量'),
        t('消耗 Quota'),
        t('消耗(USD)'),
      ],
    ];
    for (const item of quotaData) {
      // 跳过占位的空数据项
      if (item.model_name === '无数据') {
        continue;
      }
      rows.push([
        timestamp2string(item.created_at),
        item.model_name || '',
        String(item.count ?? 0),
        String(item.token_used ?? 0),
        String(item.quota ?? 0),
        (Number(item.quota ?? 0) / 500000).toFixed(4),
      ]);
    }
    const filename = genExportFilename('dashboard_report', 'csv');
    downloadCSV(filename, rows);
  };

  return (
    <div className='flex items-center justify-between mb-4'>
      <h2
        className='text-2xl font-semibold text-gray-800 transition-opacity duration-1000 ease-in-out'
        style={{ opacity: greetingVisible ? 1 : 0 }}
      >
        {getGreeting}
      </h2>
      <div className='flex gap-3'>
        <Button
          type='tertiary'
          icon={<Search size={16} />}
          onClick={showSearchModal}
          className={`bg-green-500 hover:bg-green-600 ${ICON_BUTTON_CLASS}`}
        />
        <Button
          type='tertiary'
          icon={<RefreshCw size={16} />}
          onClick={refresh}
          loading={loading}
          className={`bg-blue-500 hover:bg-blue-600 ${ICON_BUTTON_CLASS}`}
        />
        <Button
          type='tertiary'
          icon={<Download size={16} />}
          onClick={handleExport}
          className={`bg-orange-500 hover:bg-orange-600 ${ICON_BUTTON_CLASS}`}
        />
        <Button
          type='tertiary'
          icon={<FileJson size={16} />}
          onClick={onDownloadConfig}
          className={`bg-purple-500 hover:bg-purple-600 ${ICON_BUTTON_CLASS}`}
        />
      </div>
    </div>
  );
};

export default DashboardHeader;
