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

import { API } from './api';

/**
 * 将字符串转义为 CSV 安全字段值。
 * 包含逗号、引号、换行符时用双引号包裹，内部双引号转义为两个双引号。
 */
function escapeCSVField(value) {
  if (value === null || value === undefined) {
    return '';
  }
  const str = String(value);
  if (/[",\n\r]/.test(str)) {
    return `"${str.replace(/"/g, '""')}"`;
  }
  return str;
}

/**
 * 将二维数组（首行为表头）转换为带 UTF-8 BOM 的 CSV 字符串。
 * BOM 确保 Excel 正确识别中文编码。
 */
export function buildCSV(rows) {
  if (!rows || rows.length === 0) {
    return '\uFEFF';
  }
  const lines = rows.map((row) =>
    row.map(escapeCSVField).join(','),
  );
  return '\uFEFF' + lines.join('\r\n');
}

/**
 * 在浏览器端触发 CSV 文件下载（纯前端，零依赖）。
 * @param {string} filename 下载文件名
 * @param {Array<Array>} rows 二维数组，首行为表头
 */
export function downloadCSV(filename, rows) {
  const csv = buildCSV(rows);
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  link.style.display = 'none';
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  // 延迟释放，避免部分浏览器下载未完成
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/**
 * 调用后端导出接口并触发文件下载。
 * 用于大数据量流式导出（明细/聚合），后端返回 CSV blob。
 * @param {string} url 后端导出接口路径（含 query 参数）
 * @param {string} filename 下载文件名
 * @returns {Promise<void>}
 */
export async function exportFromAPI(url, filename) {
  const res = await API.get(url, {
    responseType: 'blob',
  });
  // 后端直接返回 CSV 流；若意外返回 JSON 错误，blob.type 为 application/json
  if (
    res.data &&
    res.data.type &&
    res.data.type.includes('application/json')
  ) {
    // 尝试解析错误信息
    const text = await res.data.text();
    try {
      const json = JSON.parse(text);
      throw new Error(json.message || '导出失败');
    } catch (e) {
      throw new Error(text || '导出失败');
    }
  }
  const blob = new Blob([res.data], {
    type: 'text/csv;charset=utf-8;',
  });
  const downloadUrl = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = downloadUrl;
  link.download = filename;
  link.style.display = 'none';
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  setTimeout(() => URL.revokeObjectURL(downloadUrl), 1000);
}

/**
 * 根据当前时间生成带时间戳的导出文件名。
 */
export function genExportFilename(prefix, ext = 'csv') {
  const now = new Date();
  const pad = (n) => String(n).padStart(2, '0');
  const ts = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(
    now.getDate(),
  )}_${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
  return `${prefix}_${ts}.${ext}`;
}
