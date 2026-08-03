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

import React, { useMemo, useCallback } from 'react';
import { Empty, Pagination } from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import PricingCard from './PricingCard';
import PricingCardSkeleton from './PricingCardSkeleton';
import { useMinimumLoadingTime } from '../../../../../hooks/common/useMinimumLoadingTime';
import { useIsMobile } from '../../../../../hooks/common/useIsMobile';

const PricingCardView = ({
  filteredModels,
  loading,
  rowSelection,
  pageSize,
  setPageSize,
  currentPage,
  setCurrentPage,
  selectedGroup,
  groupRatio,
  copyText,
  setModalImageUrl,
  setIsModalOpenurl,
  currency,
  siteDisplayType,
  tokenUnit,
  displayPrice,
  showRatio,
  t,
  selectedRowKeys = [],
  setSelectedRowKeys,
  openModelDetail,
}) => {
  const showSkeleton = useMinimumLoadingTime(loading);
  const isMobile = useIsMobile();

  const getModelKey = useCallback(
    (model) => model.key ?? model.model_name ?? model.id,
    [],
  );

  const paginatedModels = useMemo(() => {
    const startIndex = (currentPage - 1) * pageSize;
    return filteredModels.slice(startIndex, startIndex + pageSize);
  }, [filteredModels, currentPage, pageSize]);

  const handleCheckboxChange = useCallback(
    (model, checked) => {
      if (!setSelectedRowKeys) return;
      const modelKey = getModelKey(model);
      const newKeys = checked
        ? Array.from(new Set([...selectedRowKeys, modelKey]))
        : selectedRowKeys.filter((key) => key !== modelKey);
      setSelectedRowKeys(newKeys);
      rowSelection?.onChange?.(newKeys, null);
    },
    [selectedRowKeys, setSelectedRowKeys, rowSelection, getModelKey],
  );

  const handlePageChange = useCallback(
    (page) => setCurrentPage(page),
    [setCurrentPage],
  );

  const handlePageSizeChange = useCallback(
    (size) => {
      setPageSize(size);
    },
    [setPageSize],
  );

  // 显示骨架屏
  if (showSkeleton) {
    return (
      <PricingCardSkeleton
        rowSelection={!!rowSelection}
        showRatio={showRatio}
      />
    );
  }

  if (!filteredModels || filteredModels.length === 0) {
    return (
      <div className='flex justify-center items-center py-20'>
        <Empty
          image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
          darkModeImage={
            <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
          }
          description={t('搜索无结果')}
        />
      </div>
    );
  }

  return (
    <div className='px-2 pt-2'>
      <div className='grid grid-cols-1 xl:grid-cols-2 2xl:grid-cols-3 gap-4'>
        {paginatedModels.map((model, index) => {
          const modelKey = getModelKey(model);
          const isSelected = selectedRowKeys.includes(modelKey);

          return (
            <PricingCard
              key={modelKey || index}
              model={model}
              isSelected={isSelected}
              selectedGroup={selectedGroup}
              groupRatio={groupRatio}
              copyText={copyText}
              setModalImageUrl={setModalImageUrl}
              setIsModalOpenurl={setIsModalOpenurl}
              currency={currency}
              siteDisplayType={siteDisplayType}
              tokenUnit={tokenUnit}
              displayPrice={displayPrice}
              showRatio={showRatio}
              rowSelection={rowSelection}
              onCheckboxChange={handleCheckboxChange}
              openModelDetail={openModelDetail}
              t={t}
            />
          );
        })}
      </div>

      {/* 分页 */}
      {filteredModels.length > 0 && (
        <div className='flex justify-center mt-6 py-4 border-t pricing-pagination-divider'>
          <Pagination
            currentPage={currentPage}
            pageSize={pageSize}
            total={filteredModels.length}
            showSizeChanger={true}
            pageSizeOptions={[10, 20, 50, 100]}
            size={isMobile ? 'small' : 'default'}
            showQuickJumper={isMobile}
            onPageChange={handlePageChange}
            onPageSizeChange={handlePageSizeChange}
          />
        </div>
      )}
    </div>
  );
};

export default PricingCardView;
