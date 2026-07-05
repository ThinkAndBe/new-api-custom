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

import React, { useState, useRef } from 'react';
import { API, showError, showSuccess } from '../../../../helpers';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';
import {
  Button,
  SideSheet,
  Space,
  Spin,
  Typography,
  Card,
  Tag,
  Avatar,
  Form,
  Row,
  Col,
  Modal,
} from '@douyinfe/semi-ui';
import { IconSave, IconClose, IconUserAdd, IconCopy } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';

const { Text, Title, Paragraph } = Typography;

const AddUserModal = (props) => {
  const { t } = useTranslation();
  const formApiRef = useRef(null);
  const [loading, setLoading] = useState(false);
  // 创建成功后的邀请信息弹窗
  const [credentials, setCredentials] = useState(null);
  const isMobile = useIsMobile();

  const getInitValues = () => ({
    username: '',
    display_name: '',
    password: '',
    remark: '',
  });

  const submit = async (values) => {
    setLoading(true);
    try {
      const res = await API.post(`/api/user/`, values);
      const { success, message, data } = res.data;
      if (success) {
        showSuccess(t('用户账户创建成功！'));
        formApiRef.current?.setValues(getInitValues());
        props.refresh();
        props.handleClose();
        // 弹出邀请信息
        if (data && data.username) {
          setCredentials(data);
        }
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e.message);
    }
    setLoading(false);
  };

  const handleCancel = () => {
    props.handleClose();
  };

  // 复制邀请文案
  const handleCopyInvite = () => {
    if (!credentials) return;
    const loginUrl = window.location.origin;
    const text =
      `${t('New API 账号邀请')}\n` +
      `${t('登录地址')}: ${loginUrl}\n` +
      `${t('用户名')}: ${credentials.username}\n` +
      `${t('初始密码')}: ${credentials.password}\n` +
      `${t('首次登录需要修改密码')}`;
    navigator.clipboard.writeText(text).then(() => {
      showSuccess(t('邀请信息已复制到剪贴板'));
    });
  };

  return (
    <>
      <SideSheet
        placement={'left'}
        title={
          <Space>
            <Tag color='green' shape='circle'>
              {t('新建')}
            </Tag>
            <Title heading={4} className='m-0'>
              {t('添加用户')}
            </Title>
          </Space>
        }
        bodyStyle={{ padding: '0' }}
        visible={props.visible}
        width={isMobile ? '100%' : 600}
        footer={
          <div className='flex justify-end bg-white'>
            <Space>
              <Button
                theme='solid'
                onClick={() => formApiRef.current?.submitForm()}
                icon={<IconSave />}
                loading={loading}
              >
                {t('提交')}
              </Button>
              <Button
                theme='light'
                type='primary'
                onClick={handleCancel}
                icon={<IconClose />}
              >
                {t('取消')}
              </Button>
            </Space>
          </div>
        }
        closeIcon={null}
        onCancel={() => handleCancel()}
      >
        <Spin spinning={loading}>
          <Form
            initValues={getInitValues()}
            getFormApi={(api) => (formApiRef.current = api)}
            onSubmit={submit}
            onSubmitFail={(errs) => {
              const first = Object.values(errs)[0];
              if (first) showError(Array.isArray(first) ? first[0] : first);
              formApiRef.current?.scrollToError();
            }}
          >
            <div className='p-2'>
              <Card className='!rounded-2xl shadow-sm border-0'>
                <div className='flex items-center mb-2'>
                  <Avatar size='small' color='blue' className='mr-2 shadow-md'>
                    <IconUserAdd size={16} />
                  </Avatar>
                  <div>
                    <Text className='text-lg font-medium'>{t('用户信息')}</Text>
                    <div className='text-xs text-gray-600'>
                      {t('创建新用户账户')}
                    </div>
                  </div>
                </div>

                <Row gutter={12}>
                  <Col span={24}>
                    <Form.Input
                      field='username'
                      label={t('用户名')}
                      placeholder={t('请输入用户名')}
                      rules={[{ required: true, message: t('请输入用户名') }]}
                      showClear
                    />
                  </Col>
                  <Col span={24}>
                    <Form.Input
                      field='display_name'
                      label={t('显示名称')}
                      placeholder={t('请输入显示名称')}
                      showClear
                    />
                  </Col>
                  <Col span={24}>
                    <Form.Input
                      field='password'
                      label={t('初始密码')}
                      type='password'
                      placeholder={t('用户首次登录需要修改此密码')}
                      rules={[{ required: true, message: t('请输入初始密码') }]}
                      showClear
                    />
                  </Col>
                  <Col span={24}>
                    <Form.Input
                      field='remark'
                      label={t('备注')}
                      placeholder={t('请输入备注（仅管理员可见）')}
                      showClear
                    />
                  </Col>
                </Row>
              </Card>
            </div>
          </Form>
        </Spin>
      </SideSheet>

      {/* 邀请信息弹窗 */}
      <Modal
        title={
          <Space>
            <IconUserAdd />
            {t('用户创建成功 - 邀请信息')}
          </Space>
        }
        visible={!!credentials}
        onCancel={() => setCredentials(null)}
        footer={
          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
            <Button onClick={() => setCredentials(null)}>{t('关闭')}</Button>
            <Button
              theme='solid'
              type='primary'
              icon={<IconCopy />}
              onClick={handleCopyInvite}
            >
              {t('复制邀请信息')}
            </Button>
          </div>
        }
      >
        {credentials && (
          <div>
            <div
              style={{
                background: 'var(--semi-color-fill-0)',
                padding: 16,
                borderRadius: 8,
                marginBottom: 12,
              }}
            >
              <div style={{ marginBottom: 8 }}>
                <Text strong>{t('登录地址')}: </Text>
                <Text copyable>{window.location.origin}</Text>
              </div>
              <div style={{ marginBottom: 8 }}>
                <Text strong>{t('用户名')}: </Text>
                <Text code>{credentials.username}</Text>
              </div>
              <div style={{ marginBottom: 8 }}>
                <Text strong>{t('初始密码')}: </Text>
                <Text code>{credentials.password}</Text>
              </div>
            </div>
            <div
              style={{
                background: 'var(--semi-color-warning-light)',
                padding: 12,
                borderRadius: 8,
                fontSize: 13,
              }}
            >
              ⚠️ {t('该用户首次登录时需要修改密码才能使用系统')}
            </div>
          </div>
        )}
      </Modal>
    </>
  );
};

export default AddUserModal;
