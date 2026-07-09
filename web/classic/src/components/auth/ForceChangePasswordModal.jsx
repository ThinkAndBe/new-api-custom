import React, { useState } from 'react';
import { Modal, Form, Typography, Banner, Button } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';

const { Text } = Typography;

const ForceChangePasswordModal = ({
  visible,
  username,
  initialPassword,
  onClose,
  onSuccess,
}) => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [formApi, setFormApi] = useState(null);

  const handleSubmit = async (values) => {
    if (values.newPassword !== values.confirmPassword) {
      showError(t('两次输入的密码不一致'));
      return;
    }
    if (values.newPassword.length < 8) {
      showError(t('密码长度至少为8个字符'));
      return;
    }

    setLoading(true);
    try {
      const res = await API.post('/api/user/login/change_password', {
        username: username,
        original_password: initialPassword || values.originalPassword || '',
        new_password: values.newPassword,
      });
      const { success, message, data } = res.data;
      if (success) {
        showSuccess(t('密码修改成功，正在登录...'));
        // 后端已自动完成登录，data 中包含用户信息
        if (onSuccess) {
          onSuccess(data);
        }
      } else {
        showError(message || t('密码修改失败'));
      }
    } catch (e) {
      showError(e.message || t('网络错误'));
    }
    setLoading(false);
  };

  const handleSubmitClick = () => {
    if (formApi) {
      formApi.submitForm();
    }
  };

  return (
    <Modal
      title={t('首次登录 - 修改密码')}
      visible={visible}
      closable={false}
      maskClosable={false}
      closeOnEsc={false}
      footer={null}
      width={420}
    >
      <div style={{ marginBottom: 16 }}>
        <Banner
          type='info'
          description={t(
            '欢迎！为了账户安全，首次登录需要设置新密码。修改成功后将自动登录。',
          )}
        />
      </div>

      <Form
        getFormApi={setFormApi}
        onSubmit={handleSubmit}
        labelPosition='top'
      >
        {!initialPassword && (
          <Form.Input
            field='originalPassword'
            label={t('原密码（初始密码）')}
            type='password'
            rules={[{ required: true, message: t('请输入原密码') }]}
            showClear
          />
        )}

        <Form.Input
          field='newPassword'
          label={t('新密码')}
          type='password'
          placeholder={t('至少 8 个字符')}
          rules={[{ required: true, message: t('请输入新密码') }]}
          showClear
        />

        <Form.Input
          field='confirmPassword'
          label={t('确认新密码')}
          type='password'
          placeholder={t('再次输入新密码')}
          rules={[{ required: true, message: t('请确认新密码') }]}
          showClear
        />

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 16 }}>
          <Button
            type='primary'
            theme='solid'
            loading={loading}
            onClick={handleSubmitClick}
          >
            {t('确认修改并登录')}
          </Button>
        </div>
      </Form>
    </Modal>
  );
};

export default ForceChangePasswordModal;
