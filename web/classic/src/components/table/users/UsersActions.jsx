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
import { Button, Popconfirm } from '@douyinfe/semi-ui';
import { IconUpload, IconDownload, IconUserAdd, IconRefresh } from '@douyinfe/semi-icons';

const UsersActions = ({
  setShowAddUser,
  setShowImportUser,
  exportUsers,
  syncEmployeeIds,
  t,
}) => {
  return (
    <div className='flex gap-2 w-full md:w-auto order-2 md:order-1'>
      <Button
        className='w-full md:w-auto'
        icon={<IconUserAdd />}
        onClick={() => setShowAddUser(true)}
        size='small'
      >
        {t('添加用户')}
      </Button>
      <Button
        className='w-full md:w-auto'
        icon={<IconUpload />}
        onClick={() => setShowImportUser(true)}
        size='small'
      >
        {t('导入用户')}
      </Button>
      <Button
        className='w-full md:w-auto'
        icon={<IconDownload />}
        onClick={exportUsers}
        size='small'
      >
        {t('导出用户')}
      </Button>
      <Popconfirm
        title={t('从零信任用户目录按姓名匹配并回填工号（仅限同步分组），同名歧义会跳过并列出')}
        onConfirm={syncEmployeeIds}
      >
        <Button className='w-full md:w-auto' icon={<IconRefresh />} size='small'>
          {t('同步零信任工号')}
        </Button>
      </Popconfirm>
    </div>
  );
};

export default UsersActions;
