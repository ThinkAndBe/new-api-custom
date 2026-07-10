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

import React, { useContext, useEffect, useState, useCallback } from 'react';
import { Modal, Select, Spin, Typography } from '@douyinfe/semi-ui';
import { API, showError, showSuccess, getRelativeTime } from '../../helpers';
import { UserContext } from '../../context/User';
import { StatusContext } from '../../context/Status';

import DashboardHeader from './DashboardHeader';
import StatsCards from './StatsCards';
import ChartsPanel from './ChartsPanel';
import ApiInfoPanel from './ApiInfoPanel';
import AnnouncementsPanel from './AnnouncementsPanel';
import FaqPanel from './FaqPanel';
import UptimePanel from './UptimePanel';
import SearchModal from './modals/SearchModal';

import { useDashboardData } from '../../hooks/dashboard/useDashboardData';
import { useDashboardStats } from '../../hooks/dashboard/useDashboardStats';
import { useDashboardCharts } from '../../hooks/dashboard/useDashboardCharts';

import {
  CHART_CONFIG,
  CARD_PROPS,
  FLEX_CENTER_GAP2,
  ILLUSTRATION_SIZE,
  ANNOUNCEMENT_LEGEND_DATA,
  UPTIME_STATUS_MAP,
} from '../../constants/dashboard.constants';
import {
  getTrendSpec,
  handleCopyUrl,
  handleSpeedTest,
  getUptimeStatusColor,
  getUptimeStatusText,
  renderMonitorList,
} from '../../helpers/dashboard';

const Dashboard = () => {
  // ========== Context ==========
  const [userState, userDispatch] = useContext(UserContext);
  const [statusState, statusDispatch] = useContext(StatusContext);

  // ========== 主要数据管理 ==========
  const dashboardData = useDashboardData(userState, userDispatch, statusState);

  // ========== 图表管理 ==========
  const dashboardCharts = useDashboardCharts(
    dashboardData.dataExportDefaultTime,
    dashboardData.setTrendData,
    dashboardData.setConsumeQuota,
    dashboardData.setTimes,
    dashboardData.setConsumeTokens,
    dashboardData.setPieData,
    dashboardData.setLineData,
    dashboardData.setModelColors,
    dashboardData.t,
  );

  // ========== 统计数据 ==========
  const { groupedStatsData } = useDashboardStats(
    userState,
    dashboardData.consumeQuota,
    dashboardData.consumeTokens,
    dashboardData.times,
    dashboardData.trendData,
    dashboardData.performanceMetrics,
    dashboardData.navigate,
    dashboardData.t,
  );

  // ========== 数据处理 ==========
  const loadUserData = async () => {
    if (dashboardData.isAdminUser) {
      const userData = await dashboardData.loadUserQuotaData();
      if (userData && userData.length > 0) {
        dashboardCharts.updateUserChartData(userData);
      }
    }
  };

  const loadChannelData = async () => {
    if (dashboardData.isAdminUser) {
      const channelData = await dashboardData.loadChannelQuotaData();
      dashboardCharts.updateChannelChartData(channelData || []);
    }
  };

  const initChart = async () => {
    await dashboardData.loadQuotaData().then((data) => {
      if (data && data.length > 0) {
        dashboardCharts.updateChartData(data);
      }
    });
    await loadUserData();
    await loadChannelData();
    await dashboardData.loadUptimeData();
  };

  const handleRefresh = async () => {
    const data = await dashboardData.refresh();
    if (data && data.length > 0) {
      dashboardCharts.updateChartData(data);
    }
    await loadUserData();
    await loadChannelData();
  };

  const handleSearchConfirm = async () => {
    await dashboardData.handleSearchConfirm(dashboardCharts.updateChartData);
    await loadUserData();
    await loadChannelData();
  };

  // ========== 数据准备 ==========
  const apiInfoData = statusState?.status?.api_info || [];
  const announcementData = (statusState?.status?.announcements || []).map(
    (item) => {
      const pubDate = item?.publishDate ? new Date(item.publishDate) : null;
      const absoluteTime =
        pubDate && !isNaN(pubDate.getTime())
          ? `${pubDate.getFullYear()}-${String(pubDate.getMonth() + 1).padStart(2, '0')}-${String(pubDate.getDate()).padStart(2, '0')} ${String(pubDate.getHours()).padStart(2, '0')}:${String(pubDate.getMinutes()).padStart(2, '0')}`
          : item?.publishDate || '';
      const relativeTime = getRelativeTime(item.publishDate);
      return {
        ...item,
        time: absoluteTime,
        relative: relativeTime,
      };
    },
  );
  const faqData = statusState?.status?.faq || [];

  const uptimeLegendData = Object.entries(UPTIME_STATUS_MAP).map(
    ([status, info]) => ({
      status: Number(status),
      color: info.color,
      label: dashboardData.t(info.label),
    }),
  );

  // ========== Effects ==========
  useEffect(() => {
    initChart();
  }, []);

  // ========== 模型配置下载 ==========
  const [configModalVisible, setConfigModalVisible] = useState(false);
  const [tokens, setTokens] = useState([]);
  const [selectedTokenId, setSelectedTokenId] = useState('');
  const [tokenKey, setTokenKey] = useState('');
  const [loadingTokens, setLoadingTokens] = useState(false);
  const [loadingKey, setLoadingKey] = useState(false);
  const [userModels, setUserModels] = useState([]);
  const [statusState] = useContext(StatusContext);
  const serverAddress = statusState?.status?.server_address || '';

  const handleDownloadConfig = useCallback(async () => {
    setLoadingTokens(true);
    setConfigModalVisible(true);
    try {
      const [tokenRes, modelRes] = await Promise.all([
        API.get('/api/token/?p=1&size=100'),
        API.get('/api/user/models'),
      ]);
      if (tokenRes.data.success) {
        const items = tokenRes.data.data?.items || [];
        const active = items.filter((t) => t.status === 1);
        setTokens(active);
        if (active.length > 0) setSelectedTokenId(String(active[0].id));
      }
      if (modelRes.data.success) {
        setUserModels(modelRes.data.data || []);
      }
    } catch {
      showError('加载失败');
    } finally {
      setLoadingTokens(false);
    }
  }, []);

  // 选中令牌后获取真实 key
  useEffect(() => {
    if (!selectedTokenId || !configModalVisible) {
      setTokenKey('');
      return;
    }
    setLoadingKey(true);
    setTokenKey('');
    API.post(`/api/token/${selectedTokenId}/key`)
      .then((res) => {
        if (res.data.success) setTokenKey(res.data.data?.key || '');
        else showError(res.data.message);
      })
      .catch(() => showError('获取密钥失败'))
      .finally(() => setLoadingKey(false));
  }, [selectedTokenId, configModalVisible]);

  const doDownload = useCallback(() => {
    if (!tokenKey) {
      showError('请先选择令牌');
      return;
    }
    const baseUrl = serverAddress
      ? serverAddress.replace(/\/$/, '')
      : window.location.origin;
    const models = userModels.map((name) => {
      const supportsReasoning = /think|reason|o1|o3|o4|glm-5|deepseek-v4|qwen3/i.test(name);
      const supportsToolCall = !/embed|rerank|tts|whisper|dall|midjourney|stable/i.test(name);
      const supportsImages = /vision|glm-4v|gpt-4o|claude|gemini|qwen-vl/i.test(name);
      return {
        id: name,
        name: name,
        provider: 'openai',
        url: `${baseUrl}/v1`,
        apiKey: tokenKey,
        maxInputTokens: 1000000,
        maxOutputTokens: 64000,
        supportsToolCall,
        supportsImages,
        supportsReasoning,
      };
    });
    const json = JSON.stringify({ models }, null, 2);
    const blob = new Blob([json], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'models.json';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    setConfigModalVisible(false);
    showSuccess('配置文件已下载');
  }, [tokenKey, serverAddress, userModels]);

  const t = dashboardData.t || ((s) => s);
  const tokenOptions = tokens.map((tk) => ({
    value: String(tk.id),
    label: tk.name || `Token #${tk.id}`,
  }));

  return (
    <div className='h-full'>
      <DashboardHeader
        getGreeting={dashboardData.getGreeting}
        greetingVisible={dashboardData.greetingVisible}
        showSearchModal={dashboardData.showSearchModal}
        refresh={handleRefresh}
        loading={dashboardData.loading}
        quotaData={dashboardData.quotaData}
        inputs={dashboardData.inputs}
        onDownloadConfig={handleDownloadConfig}
        t={dashboardData.t}
      />

      <SearchModal
        searchModalVisible={dashboardData.searchModalVisible}
        handleSearchConfirm={handleSearchConfirm}
        handleCloseModal={dashboardData.handleCloseModal}
        isMobile={dashboardData.isMobile}
        isAdminUser={dashboardData.isAdminUser}
        inputs={dashboardData.inputs}
        dataExportDefaultTime={dashboardData.dataExportDefaultTime}
        timeOptions={dashboardData.timeOptions}
        handleInputChange={dashboardData.handleInputChange}
        t={dashboardData.t}
      />

      <StatsCards
        groupedStatsData={groupedStatsData}
        loading={dashboardData.loading}
        getTrendSpec={getTrendSpec}
        CARD_PROPS={CARD_PROPS}
        CHART_CONFIG={CHART_CONFIG}
      />

      {/* API信息和图表面板 */}
      <div className='mb-4'>
        <div
          className={`grid grid-cols-1 gap-4 ${dashboardData.hasApiInfoPanel ? 'lg:grid-cols-4' : ''}`}
        >
          <ChartsPanel
            activeChartTab={dashboardData.activeChartTab}
            setActiveChartTab={dashboardData.setActiveChartTab}
            spec_line={dashboardCharts.spec_line}
            spec_model_line={dashboardCharts.spec_model_line}
            spec_pie={dashboardCharts.spec_pie}
            spec_rank_bar={dashboardCharts.spec_rank_bar}
              spec_user_rank={dashboardCharts.spec_user_rank}
              spec_user_trend={dashboardCharts.spec_user_trend}
              spec_user_token_rank={dashboardCharts.spec_user_token_rank}
              spec_channel_token_rank={dashboardCharts.spec_channel_token_rank}
            isAdminUser={dashboardData.isAdminUser}
            CARD_PROPS={CARD_PROPS}
            CHART_CONFIG={CHART_CONFIG}
            FLEX_CENTER_GAP2={FLEX_CENTER_GAP2}
            hasApiInfoPanel={dashboardData.hasApiInfoPanel}
            t={dashboardData.t}
          />

          {dashboardData.hasApiInfoPanel && (
            <ApiInfoPanel
              apiInfoData={apiInfoData}
              handleCopyUrl={(url) => handleCopyUrl(url, dashboardData.t)}
              handleSpeedTest={handleSpeedTest}
              CARD_PROPS={CARD_PROPS}
              FLEX_CENTER_GAP2={FLEX_CENTER_GAP2}
              ILLUSTRATION_SIZE={ILLUSTRATION_SIZE}
              t={dashboardData.t}
            />
          )}
        </div>
      </div>

      {/* 系统公告和常见问答卡片 */}
      {dashboardData.hasInfoPanels && (
        <div className='mb-4'>
          <div className='grid grid-cols-1 lg:grid-cols-4 gap-4'>
            {/* 公告卡片 */}
            {dashboardData.announcementsEnabled && (
              <AnnouncementsPanel
                announcementData={announcementData}
                announcementLegendData={ANNOUNCEMENT_LEGEND_DATA.map(
                  (item) => ({
                    ...item,
                    label: dashboardData.t(item.label),
                  }),
                )}
                CARD_PROPS={CARD_PROPS}
                ILLUSTRATION_SIZE={ILLUSTRATION_SIZE}
                t={dashboardData.t}
              />
            )}

            {/* 常见问答卡片 */}
            {dashboardData.faqEnabled && (
              <FaqPanel
                faqData={faqData}
                CARD_PROPS={CARD_PROPS}
                FLEX_CENTER_GAP2={FLEX_CENTER_GAP2}
                ILLUSTRATION_SIZE={ILLUSTRATION_SIZE}
                t={dashboardData.t}
              />
            )}

            {/* 服务可用性卡片 */}
            {dashboardData.uptimeEnabled && (
              <UptimePanel
                uptimeData={dashboardData.uptimeData}
                uptimeLoading={dashboardData.uptimeLoading}
                activeUptimeTab={dashboardData.activeUptimeTab}
                setActiveUptimeTab={dashboardData.setActiveUptimeTab}
                loadUptimeData={dashboardData.loadUptimeData}
                uptimeLegendData={uptimeLegendData}
                renderMonitorList={(monitors) =>
                  renderMonitorList(
                    monitors,
                    (status) => getUptimeStatusColor(status, UPTIME_STATUS_MAP),
                    (status) =>
                      getUptimeStatusText(
                        status,
                        UPTIME_STATUS_MAP,
                        dashboardData.t,
                      ),
                    dashboardData.t,
                  )
                }
                CARD_PROPS={CARD_PROPS}
                ILLUSTRATION_SIZE={ILLUSTRATION_SIZE}
                t={dashboardData.t}
              />
            )}
          </div>
        </div>
      )}

      <Modal
        title={t('下载模型配置')}
        visible={configModalVisible}
        onCancel={() => setConfigModalVisible(false)}
        onOk={doDownload}
        okText={t('下载 models.json')}
        width={480}
        centered
      >
        {loadingTokens ? (
          <div style={{ textAlign: 'center', padding: 20 }}>
            <Spin />
          </div>
        ) : tokens.length === 0 ? (
          <Typography.Text type='danger'>
            {t('暂无可用令牌，请先创建令牌')}
          </Typography.Text>
        ) : (
          <div>
            <Typography.Text
              strong
              style={{ display: 'block', marginBottom: 8 }}
            >
              {t('选择令牌')}
            </Typography.Text>
            <Select
              value={selectedTokenId}
              onChange={setSelectedTokenId}
              style={{ width: '100%' }}
              optionList={tokenOptions}
              placeholder={t('请选择令牌')}
            />
            <div style={{ marginTop: 12 }}>
              {loadingKey ? (
                <Spin size='small' />
              ) : tokenKey ? (
                <Typography.Text type='success'>
                  {t('共')} {userModels.length} {t('个模型可用，点击下方按钮下载')}
                </Typography.Text>
              ) : (
                <Typography.Text type='tertiary'>
                  {t('选择令牌后自动获取密钥')}
                </Typography.Text>
              )}
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
};

export default Dashboard;
