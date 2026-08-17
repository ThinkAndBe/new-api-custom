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

import React, { useMemo, useState } from 'react';
import { Button, Empty, InputNumber, Modal, Select, Space } from '@douyinfe/semi-ui';
import CardTable from '../../common/ui/CardTable';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { getUsersColumns } from './UsersColumnDefs';
import PromoteUserModal from './modals/PromoteUserModal';
import DemoteUserModal from './modals/DemoteUserModal';
import EnableDisableUserModal from './modals/EnableDisableUserModal';
import DeleteUserModal from './modals/DeleteUserModal';
import ResetPasskeyModal from './modals/ResetPasskeyModal';
import ResetTwoFAModal from './modals/ResetTwoFAModal';
import UserSubscriptionsModal from './modals/UserSubscriptionsModal';
import { API, showError, showSuccess } from '../../../helpers';

const UsersTable = (usersData) => {
  const {
    users,
    loading,
    activePage,
    pageSize,
    userCount,
    compactMode,
    handlePageChange,
    handlePageSizeChange,
    handleRow,
    setEditingUser,
    setShowEditUser,
    manageUser,
    manageUserBatch,
    refresh,
    resetUserPasskey,
    resetUserTwoFA,
    selectedRowKeys,
    setSelectedRowKeys,
    t,
  } = usersData;

  // Modal states
  const [showPromoteModal, setShowPromoteModal] = useState(false);
  const [showDemoteModal, setShowDemoteModal] = useState(false);
  const [showEnableDisableModal, setShowEnableDisableModal] = useState(false);
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const [modalUser, setModalUser] = useState(null);
  const [enableDisableAction, setEnableDisableAction] = useState('');
  const [showResetPasskeyModal, setShowResetPasskeyModal] = useState(false);
  const [showResetTwoFAModal, setShowResetTwoFAModal] = useState(false);
  const [showUserSubscriptionsModal, setShowUserSubscriptionsModal] =
    useState(false);

  // Modal handlers
  const showPromoteUserModal = (user) => {
    setModalUser(user);
    setShowPromoteModal(true);
  };

  const showDemoteUserModal = (user) => {
    setModalUser(user);
    setShowDemoteModal(true);
  };

  const showEnableDisableUserModal = (user, action) => {
    setModalUser(user);
    setEnableDisableAction(action);
    setShowEnableDisableModal(true);
  };

  const handleReactivateUser = async (user) => {
    try {
      const res = await API.post(`/api/user/${user.id}/reactivate`);
      if (res.data.success) {
        showSuccess(t('用户已恢复'));
        await refresh();
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e?.response?.data?.message || t('操作失败'));
    }
  };

  const handleHardDeleteUser = (user) => {
    Modal.confirm({
      title: t('彻底删除用户'),
      content: t(
        '将永久删除用户「{{username}}」及其所有关联数据（令牌、OAuth 绑定等），此操作不可恢复！确定继续吗？',
        { username: user.username },
      ),
      okText: t('彻底删除'),
      cancelText: t('取消'),
      okButtonProps: {
        type: 'danger',
      },
      onOk: async () => {
        try {
          const res = await API.delete(`/api/user/${user.id}`);
          const { success, message } = res.data;
          if (success) {
            showSuccess(t('用户已彻底删除'));
            await refresh();
          } else {
            showError(message);
          }
        } catch (e) {
          showError(e?.response?.data?.message || t('操作失败'));
        }
      },
    });
  };

  const showDeleteUserModal = (user) => {
    setModalUser(user);
    setShowDeleteModal(true);
  };

  const showResetPasskeyUserModal = (user) => {
    setModalUser(user);
    setShowResetPasskeyModal(true);
  };

  const showResetTwoFAUserModal = (user) => {
    setModalUser(user);
    setShowResetTwoFAModal(true);
  };

  const showUserSubscriptionsUserModal = (user) => {
    setModalUser(user);
    setShowUserSubscriptionsModal(true);
  };

  // Modal confirm handlers
  const handlePromoteConfirm = () => {
    manageUser(modalUser.id, 'promote', modalUser);
    setShowPromoteModal(false);
  };

  const handleDemoteConfirm = () => {
    manageUser(modalUser.id, 'demote', modalUser);
    setShowDemoteModal(false);
  };

  const handleEnableDisableConfirm = () => {
    manageUser(modalUser.id, enableDisableAction, modalUser);
    setShowEnableDisableModal(false);
  };

  const handleResetPasskeyConfirm = async () => {
    await resetUserPasskey(modalUser);
    setShowResetPasskeyModal(false);
  };

  const handleResetTwoFAConfirm = async () => {
    await resetUserTwoFA(modalUser);
    setShowResetTwoFAModal(false);
  };

  // Get all columns
  const columns = useMemo(() => {
    return getUsersColumns({
      t,
      setEditingUser,
      setShowEditUser,
      showPromoteModal: showPromoteUserModal,
      showDemoteModal: showDemoteUserModal,
      showEnableDisableModal: showEnableDisableUserModal,
      showDeleteModal: showDeleteUserModal,
      showResetPasskeyModal: showResetPasskeyUserModal,
      showResetTwoFAModal: showResetTwoFAUserModal,
      showUserSubscriptionsModal: showUserSubscriptionsUserModal,
      handleReactivate: handleReactivateUser,
      handleHardDelete: handleHardDeleteUser,
    });
  }, [
    t,
    setEditingUser,
    setShowEditUser,
    showPromoteUserModal,
    showDemoteUserModal,
    showEnableDisableUserModal,
    showDeleteUserModal,
    showResetPasskeyUserModal,
    showResetTwoFAUserModal,
    showUserSubscriptionsUserModal,
    handleReactivateUser,
    handleHardDeleteUser,
  ]);

  // Handle compact mode by removing fixed positioning
  const tableColumns = useMemo(() => {
    return compactMode
      ? columns.map((col) => {
          if (col.dataIndex === 'operate') {
            const { fixed, ...rest } = col;
            return rest;
          }
          return col;
        })
      : columns;
  }, [compactMode, columns]);

  const rowSelection = useMemo(
    () => ({
      selectedRowKeys,
      onChange: setSelectedRowKeys,
    }),
    [selectedRowKeys, setSelectedRowKeys],
  );

  // ---- 批量操作 ----
  const confirmBatch = (action, titleKey, contentKey) => {
    Modal.confirm({
      title: t(titleKey, { count: selectedRowKeys.length }),
      content: t(contentKey, { count: selectedRowKeys.length }),
      okText: t('确定'),
      cancelText: t('取消'),
      okButtonProps: action === 'delete' ? { type: 'danger' } : undefined,
      onOk: () => manageUserBatch(selectedRowKeys, action),
    });
  };

  const showBatchQuotaModal = () => {
    let mode = 'add';
    let amount = 0;
    Modal.confirm({
      title: t('批量调整额度（已选 {{count}} 个用户）', {
        count: selectedRowKeys.length,
      }),
      content: (
        <div className='flex flex-col gap-3 pt-2'>
          <Select
            defaultValue='add'
            onChange={(v) => (mode = v)}
            style={{ width: '100%' }}
            optionList={[
              { value: 'add', label: t('增加额度') },
              { value: 'subtract', label: t('减少额度') },
              { value: 'override', label: t('设为（覆盖）') },
            ]}
          />
          <InputNumber
            defaultValue={0}
            min={0}
            precision={2}
            onChange={(v) => (amount = v || 0)}
            suffix={t('元')}
            style={{ width: '100%' }}
          />
        </div>
      ),
      okText: t('确定'),
      cancelText: t('取消'),
      onOk: async () => {
        // 人民币 → quota（¥1 = 50万 quota）
        const quota = Math.round((amount || 0) * 500000);
        await manageUserBatch(selectedRowKeys, 'add_quota', quota, mode);
      },
    });
  };

  return (
    <>
      {selectedRowKeys.length > 0 && (
        <div
          className='flex flex-wrap items-center gap-2 mb-2 p-2 rounded-lg'
          style={{ background: 'var(--semi-color-fill-0)' }}
        >
          <span className='text-sm font-medium'>
            {t('已选 {{count}} 个用户', { count: selectedRowKeys.length })}
          </span>
          <Space>
            <Button size='small' onClick={() => confirmBatch('enable', '批量启用用户', '将启用所选的 {{count}} 个用户。')}>
              {t('批量启用')}
            </Button>
            <Button size='small' onClick={() => confirmBatch('disable', '批量禁用用户', '将禁用所选的 {{count}} 个用户，禁用后其令牌立即失效。')}>
              {t('批量禁用')}
            </Button>
            <Button size='small' type='danger' theme='light' onClick={() => confirmBatch('delete', '批量注销用户', '将注销所选的 {{count}} 个用户（软删除，可恢复）。')}>
              {t('批量注销')}
            </Button>
            <Button size='small' onClick={showBatchQuotaModal}>
              {t('批量调额度')}
            </Button>
            <Button size='small' theme='borderless' onClick={() => setSelectedRowKeys([])}>
              {t('取消选择')}
            </Button>
          </Space>
        </div>
      )}
      <CardTable
        columns={tableColumns}
        dataSource={users}
        rowSelection={rowSelection}
        scroll={compactMode ? undefined : { x: 'max-content' }}
        pagination={{
          currentPage: activePage,
          pageSize: pageSize,
          total: userCount,
          pageSizeOpts: [10, 20, 50, 100],
          showSizeChanger: true,
          onPageSizeChange: handlePageSizeChange,
          onPageChange: handlePageChange,
        }}
        hidePagination={true}
        loading={loading}
        onRow={handleRow}
        empty={
          <Empty
            image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
            darkModeImage={
              <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
            }
            description={t('搜索无结果')}
            style={{ padding: 30 }}
          />
        }
        className='overflow-hidden'
        size='middle'
      />

      {/* Modal components */}
      <PromoteUserModal
        visible={showPromoteModal}
        onCancel={() => setShowPromoteModal(false)}
        onConfirm={handlePromoteConfirm}
        user={modalUser}
        t={t}
      />

      <DemoteUserModal
        visible={showDemoteModal}
        onCancel={() => setShowDemoteModal(false)}
        onConfirm={handleDemoteConfirm}
        user={modalUser}
        t={t}
      />

      <EnableDisableUserModal
        visible={showEnableDisableModal}
        onCancel={() => setShowEnableDisableModal(false)}
        onConfirm={handleEnableDisableConfirm}
        user={modalUser}
        action={enableDisableAction}
        t={t}
      />

      <DeleteUserModal
        visible={showDeleteModal}
        onCancel={() => setShowDeleteModal(false)}
        user={modalUser}
        users={users}
        activePage={activePage}
        refresh={refresh}
        manageUser={manageUser}
        t={t}
      />

      <ResetPasskeyModal
        visible={showResetPasskeyModal}
        onCancel={() => setShowResetPasskeyModal(false)}
        onConfirm={handleResetPasskeyConfirm}
        user={modalUser}
        t={t}
      />

      <ResetTwoFAModal
        visible={showResetTwoFAModal}
        onCancel={() => setShowResetTwoFAModal(false)}
        onConfirm={handleResetTwoFAConfirm}
        user={modalUser}
        t={t}
      />

      <UserSubscriptionsModal
        visible={showUserSubscriptionsModal}
        onCancel={() => setShowUserSubscriptionsModal(false)}
        user={modalUser}
        t={t}
        onSuccess={() => refresh?.()}
      />
    </>
  );
};

export default UsersTable;
