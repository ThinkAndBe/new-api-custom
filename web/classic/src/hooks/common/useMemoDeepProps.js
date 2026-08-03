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

import { useMemo, useRef } from 'react';

/**
 * 轻量深比较：仅覆盖本项目 props 中的基础类型/数组/纯对象，
 * 忽略函数与 React 元素（不做序列化，直接按引用比较）。
 */
const shallowEqualValue = (a, b) => {
  if (a === b) return true;
  if (typeof a !== 'object' || typeof b !== 'object' || a == null || b == null)
    return false;

  if (Array.isArray(a) !== Array.isArray(b)) return false;

  const keysA = Object.keys(a);
  const keysB = Object.keys(b);
  if (keysA.length !== keysB.length) return false;

  for (const key of keysA) {
    if (!Object.prototype.hasOwnProperty.call(b, key)) return false;
    const va = a[key];
    const vb = b[key];
    // 函数 / React 元素：只做引用比较
    if (typeof va === 'function' || (va && va.$$typeof)) {
      if (va !== vb) return false;
      continue;
    }
    if (Array.isArray(va) || (typeof va === 'object' && va !== null)) {
      if (!shallowEqualValue(va, vb)) return false;
      continue;
    }
    if (va !== vb) return false;
  }
  return true;
};

/**
 * 基于浅层深比较的 memo：当 props 在结构层面未变化时复用旧引用。
 * 适合 useModelPricingData 这类返回大对象但字段多数稳定的场景。
 */
export const useMemoDeepProps = (value) => {
  const ref = useRef(value);
  if (!shallowEqualValue(ref.current, value)) {
    ref.current = value;
  }
  return ref.current;
};
