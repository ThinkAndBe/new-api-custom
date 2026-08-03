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

import React, { useState, useMemo } from 'react';
import { Layout, ImagePreview } from '@douyinfe/semi-ui';
import PricingSidebar from './PricingSidebar';
import PricingContent from './content/PricingContent';
import ModelDetailSideSheet from '../modal/ModelDetailSideSheet';
import { useModelPricingData } from '../../../../hooks/model-pricing/useModelPricingData';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';
import { useMemoDeepProps } from '../../../../hooks/common/useMemoDeepProps';

const PricingPage = () => {
  const pricingData = useModelPricingData();
  const { Sider, Content } = Layout;
  const isMobile = useIsMobile();
  const [showRatio, setShowRatio] = useState(false);
  const [viewMode, setViewMode] = useState('card');

  // 基于浅层深比较，确保模型数据未变化时引用稳定，避免 PricingSidebar 等被重复渲染
  const stablePricingData = useMemoDeepProps(pricingData);

  const allProps = useMemo(
    () => ({
      ...stablePricingData,
      showRatio,
      setShowRatio,
      viewMode,
      setViewMode,
    }),
    [stablePricingData, showRatio, viewMode],
  );

  return (
    <div className='bg-white dark:bg-slate-900'>
      <Layout className='pricing-layout'>
        {!isMobile && (
          <Sider className='pricing-scroll-hide pricing-sidebar'>
            <PricingSidebar {...allProps} />
          </Sider>
        )}

        <Content className='pricing-scroll-hide pricing-content'>
          <PricingContent
            {...allProps}
            isMobile={isMobile}
            sidebarProps={allProps}
          />
        </Content>
      </Layout>

      <ImagePreview
        src={pricingData.modalImageUrl}
        visible={pricingData.isModalOpenurl}
        onVisibleChange={(visible) => pricingData.setIsModalOpenurl(visible)}
      />

      <ModelDetailSideSheet
        visible={pricingData.showModelDetail}
        onClose={pricingData.closeModelDetail}
        modelData={pricingData.selectedModel}
        groupRatio={pricingData.groupRatio}
        usableGroup={pricingData.usableGroup}
        currency={pricingData.currency}
        siteDisplayType={pricingData.siteDisplayType}
        tokenUnit={pricingData.tokenUnit}
        displayPrice={pricingData.displayPrice}
        showRatio={allProps.showRatio}
        vendorsMap={pricingData.vendorsMap}
        endpointMap={pricingData.endpointMap}
        autoGroups={pricingData.autoGroups}
        t={pricingData.t}
      />
    </div>
  );
};

export default PricingPage;
