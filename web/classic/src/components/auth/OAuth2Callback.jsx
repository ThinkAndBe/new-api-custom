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

import React, { useContext, useEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Button } from '@douyinfe/semi-ui';
import {
  API,
  showError,
  showSuccess,
  updateAPI,
  setUserData,
} from '../../helpers';
import { UserContext } from '../../context/User';
import Loading from '../common/ui/Loading';

// aTrust SSO 恢复：finish 拉登录态失败时自动重启 SSO 的次数上限（会话级）。
// aTrust 反代注入的 web_proxy.js 会偶发让 fetch 假 401（服务端会话其实正常），
// 原地重试通常即过；重启 SSO 是给「会话确实缺失」的场景换一条完整链路。
// 上限 1 次防死循环，之后交给用户手动点重试按钮。
const SSO_RESTART_KEY = 'atrust_sso_restarts';
const SSO_RESTART_LIMIT = 1;

const OAuth2Callback = (props) => {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const [, userDispatch] = useContext(UserContext);
  const navigate = useNavigate();

  // SSO finish 失败且自动恢复用尽后的错误态（渲染重试按钮而不是弹回 /login）
  const [ssoFailed, setSsoFailed] = useState(false);
  const [ssoError, setSsoError] = useState('');

  // 防止 React 18 Strict Mode 下重复执行
  const hasExecuted = useRef(false);

  // 最大重试次数
  const MAX_RETRIES = 3;

  const sendCode = async (code, state, retry = 0) => {
    try {
      const { data: resData } = await API.get(
        `/api/oauth/${props.type}?code=${code}&state=${state}`,
      );

      const { success, message, data } = resData;

      if (!success) {
        // 业务错误不重试，直接显示错误
        showError(message || t('授权失败'));
        return;
      }

      if (data?.action === 'bind') {
        showSuccess(t('绑定成功！'));
        navigate('/console/personal');
      } else {
        userDispatch({ type: 'login', payload: data });
        localStorage.setItem('user', JSON.stringify(data));
        setUserData(data);
        updateAPI();
        showSuccess(t('登录成功！'));
        navigate('/console/token');
      }
    } catch (error) {
      // 网络错误等可重试
      if (retry < MAX_RETRIES) {
        // 递增的退避等待
        await new Promise((resolve) => setTimeout(resolve, (retry + 1) * 2000));
        return sendCode(code, state, retry + 1);
      }

      // 重试次数耗尽，提示错误并返回设置页面
      showError(error.message || t('授权失败'));
      navigate('/console/personal');
    }
  };

  // SSO finish 成功后的统一登录落位
  const ssoLogin = (data) => {
    sessionStorage.removeItem(SSO_RESTART_KEY);
    userDispatch({ type: 'login', payload: data });
    localStorage.setItem('user', JSON.stringify(data));
    setUserData(data);
    updateAPI();
    showSuccess(t('登录成功！'));
    navigate('/console/token');
  };

  // finish 失败后的恢复：自动重启一次 SSO；用尽则展示重试按钮
  const ssoRecover = (message) => {
    const restarts = Number(sessionStorage.getItem(SSO_RESTART_KEY) || 0);
    if (restarts < SSO_RESTART_LIMIT) {
      sessionStorage.setItem(SSO_RESTART_KEY, String(restarts + 1));
      window.location.href = `/api/oauth/${props.type}/start`;
      return;
    }
    setSsoError(message);
    setSsoFailed(true);
  };

  // 拉取零信任登录态：网络层失败（含假 401）原地重试 2 次，间隔 1 秒
  const finishAttempt = (retry = 0) => {
    API.get(`/api/oauth/${props.type}/finish`)
      .then(({ data: resData }) => {
        const { success, message, data } = resData;
        if (!success) {
          // 服务端无会话：重启 SSO 走完整链路重建
          ssoRecover(message || t('授权失败'));
          return;
        }
        ssoLogin(data);
      })
      .catch((error) => {
        if (retry < 2) {
          setTimeout(() => finishAttempt(retry + 1), 1000);
          return;
        }
        ssoRecover(error?.message || t('网络异常，请重试'));
      });
  };

  // 重试按钮：清掉重启计数，重新发起 SSO
  const retrySSO = () => {
    sessionStorage.removeItem(SSO_RESTART_KEY);
    window.location.href = `/api/oauth/${props.type}/start`;
  };

  useEffect(() => {
    // 防止 React 18 Strict Mode 下重复执行
    if (hasExecuted.current) {
      return;
    }
    hasExecuted.current = true;

    // aTrust 零信任 SSO 引导：服务端回调已建立会话，这里拉取登录态
    // 写入 localStorage（与密码登录的前端处理一致）
    if (searchParams.get('sso')) {
      finishAttempt();
      return;
    }

    const code = searchParams.get('code');
    const state = searchParams.get('state');

    // 参数缺失直接返回
    if (!code) {
      showError(t('未获取到授权码'));
      navigate('/console/personal');
      return;
    }

    sendCode(code, state);
  }, []);

  if (ssoFailed) {
    return (
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          gap: 16,
          minHeight: '60vh',
        }}
      >
        <div style={{ fontSize: 16, fontWeight: 600 }}>
          {t('零信任登录未完成')}
        </div>
        <div style={{ color: 'var(--semi-color-text-1)', fontSize: 14 }}>
          {ssoError}
        </div>
        <Button theme='solid' type='primary' size='large' onClick={retrySSO}>
          {t('重新登录')}
        </Button>
      </div>
    );
  }

  return <Loading />;
};

export default OAuth2Callback;
