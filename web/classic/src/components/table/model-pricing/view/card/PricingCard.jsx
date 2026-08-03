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

import React, { memo, useMemo, useCallback } from 'react';
import { Card, Tag, Tooltip, Checkbox, Button, Avatar } from '@douyinfe/semi-ui';
import { IconHelpCircle } from '@douyinfe/semi-icons';
import { Copy } from 'lucide-react';
import {
  stringToColor,
  calculateModelPrice,
  formatPriceInfo,
  formatDynamicPriceSummary,
  getLobeHubIcon,
} from '../../../../../helpers';
import { renderLimitedItems } from '../../../../common/ui/RenderUtils';
import {
  renderAvailabilityTag,
  renderCapabilityTags,
  renderContextWindow,
} from '../ModelCapabilityTags';

const CARD_STYLES = {
  container:
    'w-12 h-12 rounded-2xl flex items-center justify-center relative shadow-md',
  icon: 'w-8 h-8 flex items-center justify-center',
  selected: 'border-blue-500 dark:border-blue-400 bg-blue-50 dark:bg-blue-900/30',
  default: 'border-gray-200 dark:border-slate-700 hover:border-gray-300 dark:hover:border-slate-500',
};

// 获取模型图标
const getModelIcon = (model) => {
  if (!model || !model.model_name) {
    return (
      <div className={CARD_STYLES.container}>
        <Avatar size='large'>?</Avatar>
      </div>
    );
  }
  // 1) 优先使用模型自定义图标
  if (model.icon) {
    return (
      <div className={CARD_STYLES.container}>
        <div className={CARD_STYLES.icon}>{getLobeHubIcon(model.icon, 32)}</div>
      </div>
    );
  }
  // 2) 退化为供应商图标
  if (model.vendor_icon) {
    return (
      <div className={CARD_STYLES.container}>
        <div className={CARD_STYLES.icon}>
          {getLobeHubIcon(model.vendor_icon, 32)}
        </div>
      </div>
    );
  }

  const avatarText = model.model_name.slice(0, 2).toUpperCase();
  return (
    <div className={CARD_STYLES.container}>
      <Avatar
        size='large'
        style={{
          width: 48,
          height: 48,
          borderRadius: 16,
          fontSize: 16,
          fontWeight: 'bold',
        }}
      >
        {avatarText}
      </Avatar>
    </div>
  );
};

// 渲染标签
const renderTags = (record, t) => {
  // 计费类型标签（左边）
  let billingTag = (
    <Tag key='billing' shape='circle' color='white' size='small'>
      -
    </Tag>
  );
  if (record.quota_type === 1) {
    billingTag = (
      <Tag key='billing' shape='circle' color='teal' size='small'>
        {t('按次计费')}
      </Tag>
    );
  } else if (record.quota_type === 0) {
    billingTag = (
      <Tag key='billing' shape='circle' color='violet' size='small'>
        {t('按量计费')}
      </Tag>
    );
  }

  // 自定义标签（右边）
  const customTags = [];
  if (record.tags) {
    const tagArr = record.tags.split(',').filter(Boolean);
    tagArr.forEach((tg, idx) => {
      customTags.push(
        <Tag
          key={`custom-${idx}`}
          shape='circle'
          color={stringToColor(tg)}
          size='small'
        >
          {tg}
        </Tag>,
      );
    });
  }

  return (
    <div className='flex items-center justify-between'>
      <div className='flex items-center gap-2'>{billingTag}</div>
      <div className='flex items-center gap-1'>
        {customTags.length > 0 &&
          renderLimitedItems({
            items: customTags.map((tag, idx) => ({
              key: `custom-${idx}`,
              element: tag,
            })),
            renderItem: (item, idx) => item.element,
            maxDisplay: 3,
          })}
      </div>
    </div>
  );
};

const PricingCard = memo(
  ({
    model,
    isSelected,
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
    rowSelection,
    onCheckboxChange,
    openModelDetail,
    t,
  }) => {
    const priceData = useMemo(
      () =>
        calculateModelPrice({
          record: model,
          selectedGroup,
          groupRatio,
          tokenUnit,
          displayPrice,
          currency,
          quotaDisplayType: siteDisplayType,
        }),
      [
        model,
        selectedGroup,
        groupRatio,
        tokenUnit,
        displayPrice,
        currency,
        siteDisplayType,
      ],
    );

    const handleCopy = useCallback(
      (e) => {
        e.stopPropagation();
        copyText(model.model_name);
      },
      [copyText, model.model_name],
    );

    const handleCheckboxChange = useCallback(
      (e) => {
        e.stopPropagation();
        onCheckboxChange?.(model, e.target.checked);
      },
      [model, onCheckboxChange],
    );

    const handleCardClick = useCallback(() => {
      openModelDetail?.(model);
    }, [model, openModelDetail]);

    const handleRatioHelpClick = useCallback(
      (e) => {
        e.stopPropagation();
        setModalImageUrl('/ratio.png');
        setIsModalOpenurl(true);
      },
      [setModalImageUrl, setIsModalOpenurl],
    );

    return (
      <Card
        className={`!rounded-2xl transition-all duration-200 hover:shadow-lg border cursor-pointer ${isSelected ? CARD_STYLES.selected : CARD_STYLES.default}`}
        bodyStyle={{ height: '100%' }}
        style={model.available === false ? { opacity: 0.55 } : undefined}
        onClick={handleCardClick}
      >
        <div className='flex flex-col h-full'>
          {/* 头部：图标 + 模型名称 + 操作按钮 */}
          <div className='flex items-start justify-between mb-3'>
            <div className='flex items-start space-x-3 flex-1 min-w-0'>
              {getModelIcon(model)}
              <div className='flex-1 min-w-0'>
                <h3 className='text-lg font-bold text-gray-900 dark:text-gray-100 truncate'>
                  {model.model_name}
                </h3>
                <div className='flex flex-col gap-1 text-xs mt-1'>
                  {priceData.isDynamicPricing
                    ? formatDynamicPriceSummary(
                        priceData.billingExpr,
                        t,
                        priceData.usedGroupRatio,
                      )
                    : formatPriceInfo(priceData, t, siteDisplayType)}
                </div>
                {/* 可用状态 + 上下文窗口 + 能力标签（视觉/推理/工具调用） */}
                <div className='flex items-center gap-1 flex-wrap mt-1'>
                  {renderAvailabilityTag(model, t)}
                  {renderContextWindow(model, t)}
                </div>
                {renderCapabilityTags(model, t)}
              </div>
            </div>

            <div className='flex items-center space-x-2 ml-3'>
              {/* 复制按钮 */}
              <Button
                size='small'
                theme='outline'
                type='tertiary'
                icon={<Copy size={12} />}
                onClick={handleCopy}
              />

              {/* 选择框 */}
              {rowSelection && (
                <Checkbox
                  checked={isSelected}
                  onChange={handleCheckboxChange}
                />
              )}
            </div>
          </div>

          {/* 模型描述 - 占据剩余空间 */}
          <div className='flex-1 mb-4'>
            <p
              className='text-xs line-clamp-2 leading-relaxed'
              style={{ color: 'var(--semi-color-text-2)' }}
            >
              {model.description || ''}
            </p>
          </div>

          {/* 底部区域 */}
          <div className='mt-auto'>
            {/* 标签区域 */}
            {renderTags(model, t)}

            {/* 倍率信息（可选） */}
            {showRatio && (
              <div className='pt-3'>
                <div className='flex items-center space-x-1 mb-2'>
                  <span className='text-xs font-medium text-gray-700'>
                    {t('倍率信息')}
                  </span>
                  <Tooltip content={t('倍率是为了方便换算不同价格的模型')}>
                    <IconHelpCircle
                      className='text-blue-500 cursor-pointer'
                      size='small'
                      onClick={handleRatioHelpClick}
                    />
                  </Tooltip>
                </div>
                <div className='grid grid-cols-3 gap-2 text-xs text-gray-600'>
                  <div>
                    {t('模型')}:{' '}
                    {model.quota_type === 0 ? model.model_ratio : t('无')}
                  </div>
                  <div>
                    {t('补全')}:{' '}
                    {model.quota_type === 0
                      ? parseFloat(model.completion_ratio.toFixed(3))
                      : t('无')}
                  </div>
                  <div>
                    {t('分组')}: {priceData?.usedGroupRatio ?? '-'}
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      </Card>
    );
  },
);

PricingCard.displayName = 'PricingCard';

export default PricingCard;
