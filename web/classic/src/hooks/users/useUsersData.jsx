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

import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';
import { exportFromAPI } from '../../helpers/csv';
import { ITEMS_PER_PAGE } from '../../constants';
import { useTableCompactMode } from '../common/useTableCompactMode';

export const useUsersData = () => {
  const { t } = useTranslation();
  const [compactMode, setCompactMode] = useTableCompactMode('users');

  // State management
  const [users, setUsers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(ITEMS_PER_PAGE);
  const [searching, setSearching] = useState(false);
  const [groupOptions, setGroupOptions] = useState([]);
  const [userCount, setUserCount] = useState(0);
  const [selectedRowKeys, setSelectedRowKeys] = useState([]);

  // Modal states
  const [showAddUser, setShowAddUser] = useState(false);
  const [showEditUser, setShowEditUser] = useState(false);
  const [showImportUser, setShowImportUser] = useState(false);
  const [editingUser, setEditingUser] = useState({
    id: undefined,
  });

  // Form initial values
  const formInitValues = {
    searchKeyword: '',
    searchGroup: '',
    searchRole: undefined,
    searchStatus: undefined,
  };

  // Form API reference
  const [formApi, setFormApi] = useState(null);

  // Get form values helper function
  const getFormValues = () => {
    const formValues = formApi ? formApi.getValues() : {};
    return {
      searchKeyword: formValues.searchKeyword || '',
      searchGroup: formValues.searchGroup || '',
      searchRole: formValues.searchRole ?? '',
      searchStatus: formValues.searchStatus ?? '',
    };
  };

  // Set user format with key field
  const setUserFormat = (users) => {
    for (let i = 0; i < users.length; i++) {
      users[i].key = users[i].id;
    }
    setUsers(users);
  };

  // Load users data
  const loadUsers = async (startIdx, pageSize) => {
    setLoading(true);
    const res = await API.get(`/api/user/?p=${startIdx}&page_size=${pageSize}`);
    const { success, message, data } = res.data;
    if (success) {
      const newPageData = data.items;
      setActivePage(data.page);
      setUserCount(data.total);
      setUserFormat(newPageData);
    } else {
      showError(message);
    }
    setLoading(false);
  };

  // Search users with keyword/group/role/status
  const searchUsers = async (
    startIdx,
    pageSize,
    searchKeyword = null,
    searchGroup = null,
  ) => {
    // If no parameters passed, get values from form
    let searchRole, searchStatus;
    if (searchKeyword === null || searchGroup === null) {
      const formValues = getFormValues();
      searchKeyword = formValues.searchKeyword;
      searchGroup = formValues.searchGroup;
    }
    ({ searchRole, searchStatus } = getFormValues());

    const noFilter =
      searchKeyword === '' &&
      searchGroup === '' &&
      searchRole === '' &&
      searchStatus === '';
    if (noFilter) {
      // If all filters blank, load users instead
      await loadUsers(startIdx, pageSize);
      return;
    }
    setSearching(true);
    const params = new URLSearchParams({
      keyword: searchKeyword,
      group: searchGroup,
      p: startIdx,
      page_size: pageSize,
    });
    if (searchRole !== '') params.set('role', searchRole);
    if (searchStatus !== '') params.set('status', searchStatus);
    const res = await API.get(`/api/user/search?${params.toString()}`);
    const { success, message, data } = res.data;
    if (success) {
      const newPageData = data.items;
      setActivePage(data.page);
      setUserCount(data.total);
      setUserFormat(newPageData);
    } else {
      showError(message);
    }
    setSearching(false);
  };

  // 批量管理（启用/禁用/注销/调额度）
  const manageUserBatch = async (ids, action, value, mode) => {
    if (!ids || ids.length === 0) return;
    setLoading(true);
    try {
      const payload = { ids, action };
      if (value !== undefined) payload.value = value;
      if (mode !== undefined) payload.mode = mode;
      const res = await API.post('/api/user/manage_batch', payload);
      const { success, message, data } = res.data;
      if (success) {
        const errCount = data.total - data.success_count;
        showSuccess(
          t('批量操作完成：成功 {{success}} 个{{extra}}', {
            success: data.success_count,
            extra: errCount > 0 ? `，失败 ${errCount} 个` : '',
          }),
        );
        setSelectedRowKeys([]);
        await refresh();
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e?.response?.data?.message || t('操作失败，请重试'));
    }
    setLoading(false);
  };

  // 按当前筛选导出用户 CSV
  const exportUsers = async () => {
    const { searchKeyword, searchGroup, searchRole, searchStatus } =
      getFormValues();
    const params = new URLSearchParams();
    if (searchKeyword) params.set('keyword', searchKeyword);
    if (searchGroup) params.set('group', searchGroup);
    if (searchRole !== '') params.set('role', searchRole);
    if (searchStatus !== '') params.set('status', searchStatus);
    const qs = params.toString();
    await exportFromAPI(`/api/user/export${qs ? '?' + qs : ''}`, 'users');
  };

  // Manage user operations (promote, demote, enable, disable, delete)
  const manageUser = async (userId, action, record) => {
    // Trigger loading state to force table re-render
    setLoading(true);

    const res = await API.post('/api/user/manage', {
      id: userId,
      action,
    });

    const { success, message } = res.data;
    if (success) {
      showSuccess(t('操作成功完成！'));
      const user = res.data.data;

      // Create a new array and new object to ensure React detects changes
      const newUsers = users.map((u) => {
        if (u.id === userId) {
          if (action === 'delete') {
            return { ...u, DeletedAt: new Date() };
          }
          return { ...u, status: user.status, role: user.role };
        }
        return u;
      });

      setUsers(newUsers);
    } else {
      showError(message);
    }

    setLoading(false);
  };

  const resetUserPasskey = async (user) => {
    if (!user) {
      return;
    }
    try {
      const res = await API.delete(`/api/user/${user.id}/reset_passkey`);
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('Passkey 已重置'));
      } else {
        showError(message || t('操作失败，请重试'));
      }
    } catch (error) {
      showError(t('操作失败，请重试'));
    }
  };

  const resetUserTwoFA = async (user) => {
    if (!user) {
      return;
    }
    try {
      const res = await API.delete(`/api/user/${user.id}/2fa`);
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('二步验证已重置'));
      } else {
        showError(message || t('操作失败，请重试'));
      }
    } catch (error) {
      showError(t('操作失败，请重试'));
    }
  };

  // Handle page change
  const handlePageChange = (page) => {
    setActivePage(page);
    const { searchKeyword, searchGroup, searchRole, searchStatus } =
      getFormValues();
    if (
      searchKeyword === '' &&
      searchGroup === '' &&
      searchRole === '' &&
      searchStatus === ''
    ) {
      loadUsers(page, pageSize).then();
    } else {
      searchUsers(page, pageSize, searchKeyword, searchGroup).then();
    }
  };

  // Handle page size change
  const handlePageSizeChange = async (size) => {
    localStorage.setItem('page-size', size + '');
    setPageSize(size);
    setActivePage(1);
    loadUsers(activePage, size)
      .then()
      .catch((reason) => {
        showError(reason);
      });
  };

  // Handle table row styling for disabled/deleted users
  const handleRow = (record, index) => {
    if (record.DeletedAt !== null || record.status !== 1) {
      return {
        style: {
          background: 'var(--semi-color-disabled-border)',
        },
      };
    } else {
      return {};
    }
  };

  // Refresh data
  const refresh = async (page = activePage) => {
    const { searchKeyword, searchGroup, searchRole, searchStatus } =
      getFormValues();
    if (
      searchKeyword === '' &&
      searchGroup === '' &&
      searchRole === '' &&
      searchStatus === ''
    ) {
      await loadUsers(page, pageSize);
    } else {
      await searchUsers(page, pageSize, searchKeyword, searchGroup);
    }
  };

  // Fetch groups data
  const fetchGroups = async () => {
    try {
      let res = await API.get(`/api/group/`);
      if (res === undefined) {
        return;
      }
      setGroupOptions(
        res.data.data.map((group) => ({
          label: group,
          value: group,
        })),
      );
    } catch (error) {
      showError(error.message);
    }
  };

  // Modal control functions
  const closeAddUser = () => {
    setShowAddUser(false);
  };

  const closeEditUser = () => {
    setShowEditUser(false);
    setEditingUser({
      id: undefined,
    });
  };

  const closeImportUser = () => {
    setShowImportUser(false);
  };

  // Initialize data on component mount
  useEffect(() => {
    loadUsers(0, pageSize)
      .then()
      .catch((reason) => {
        showError(reason);
      });
    fetchGroups().then();
  }, []);

  return {
    // Data state
    users,
    loading,
    activePage,
    pageSize,
    userCount,
    searching,
    groupOptions,
    selectedRowKeys,
    setSelectedRowKeys,

    // Modal state
    showAddUser,
    showEditUser,
    showImportUser,
    editingUser,
    setShowAddUser,
    setShowEditUser,
    setShowImportUser,
    setEditingUser,

    // Form state
    formInitValues,
    formApi,
    setFormApi,

    // UI state
    compactMode,
    setCompactMode,

    // Actions
    loadUsers,
    searchUsers,
    manageUser,
    manageUserBatch,
    exportUsers,
    resetUserPasskey,
    resetUserTwoFA,
    handlePageChange,
    handlePageSizeChange,
    handleRow,
    refresh,
    closeAddUser,
    closeEditUser,
    closeImportUser,
    getFormValues,

    // Translation
    t,
  };
};
