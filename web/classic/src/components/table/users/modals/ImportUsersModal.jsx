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
import {
  Modal,
  Button,
  Typography,
  Banner,
  Tag,
  TextArea,
  Table,
  Upload,
} from '@douyinfe/semi-ui';
import { IconDownload } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../../../helpers';
import { downloadCSV } from '../../../../helpers/csv';

const { Text } = Typography;

// 简单 CSV 行解析：支持逗号分隔与双引号包裹（引号内逗号/转义引号）
const parseCSVLine = (line) => {
  const out = [];
  let cur = '';
  let inQuotes = false;
  for (let i = 0; i < line.length; i++) {
    const ch = line[i];
    if (inQuotes) {
      if (ch === '"') {
        if (i + 1 < line.length && line[i + 1] === '"') {
          cur += '"';
          i++;
        } else {
          inQuotes = false;
        }
      } else {
        cur += ch;
      }
    } else if (ch === '"') {
      inQuotes = true;
    } else if (ch === ',') {
      out.push(cur);
      cur = '';
    } else {
      cur += ch;
    }
  }
  out.push(cur);
  return out.map((s) => s.trim());
};

const TEMPLATE_HEADER = 'username,password,display_name,group,quota_cny';

const ImportUsersModal = ({ visible, handleClose, refresh, groupOptions }) => {
  const { t } = useTranslation();
  const [rawText, setRawText] = useState('');
  const [importing, setImporting] = useState(false);
  // results: null = 未导入；{summary, results}
  const [result, setResult] = useState(null);

  const parsedRows = useMemo(() => {
    const lines = rawText
      .split(/\r?\n/)
      .map((l) => l.trim())
      .filter(Boolean);
    const rows = [];
    for (const [idx, line] of lines.entries()) {
      const cols = parseCSVLine(line);
      // 跳过表头行
      if (idx === 0 && cols[0]?.toLowerCase() === 'username') continue;
      rows.push({
        row: rows.length + 1,
        username: cols[0] || '',
        password: cols[1] || '',
        display_name: cols[2] || '',
        group: cols[3] || 'default',
        quota_cny: cols[4] ? Number(cols[4]) || 0 : 0,
      });
    }
    return rows;
  }, [rawText]);

  // 内容变更后清掉上一轮结果，重新允许导入
  const updateRawText = (text) => {
    setRawText(text);
    setResult(null);
  };

  const reset = () => {
    setRawText('');
    setResult(null);
    setImporting(false);
  };

  const onClose = () => {
    reset();
    handleClose();
  };

  const handleDownloadTemplate = () => {
    const example = [
      TEMPLATE_HEADER,
      'zhangsan,Passw0rd123,张三,default,10',
      'lisi,Passw0rd456,李四,svip,',
    ].join('\n');
    downloadCSV('users_import_template.csv', [example.split('\n')]);
  };

  const handleFile = ({ file }) => {
    const reader = new FileReader();
    reader.onload = (e) => {
      updateRawText(String(e.target.result || ''));
    };
    reader.readAsText(file.fileInstance, 'utf-8');
    return { autoRemove: true, fileList: [] };
  };

  const handleImport = async () => {
    if (parsedRows.length === 0) {
      showError(t('没有可导入的行'));
      return;
    }
    setImporting(true);
    try {
      const res = await API.post('/api/user/import', {
        users: parsedRows.map((r) => ({
          username: r.username,
          password: r.password,
          display_name: r.display_name,
          group: r.group,
          quota_cny: r.quota_cny,
        })),
      });
      const { success, message, data } = res.data;
      if (success) {
        setResult(data);
        showSuccess(
          t('导入完成：成功 {{s}}，重复 {{d}}，失败 {{e}}', {
            s: data.success_count,
            d: data.duplicate,
            e: data.error_count,
          }),
        );
        refresh();
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e?.response?.data?.message || t('导入失败'));
    }
    setImporting(false);
  };

  const statusTag = (status) => {
    if (status === 'success') return <Tag color='green'>{t('成功')}</Tag>;
    if (status === 'duplicate') return <Tag color='orange'>{t('重复')}</Tag>;
    return <Tag color='red'>{t('失败')}</Tag>;
  };

  return (
    <Modal
      title={t('批量导入用户')}
      visible={visible}
      onCancel={onClose}
      width={680}
      footer={
        <div className='flex justify-between items-center w-full'>
          <Button
            theme='light'
            icon={<IconDownload />}
            onClick={handleDownloadTemplate}
          >
            {t('下载模板')}
          </Button>
          <div className='flex gap-2'>
            <Button theme='light' onClick={onClose}>
              {t('关闭')}
            </Button>
            {!result && (
              <Button
                theme='solid'
                type='primary'
                loading={importing}
                disabled={parsedRows.length === 0}
                onClick={handleImport}
              >
                {t('导入 {{count}} 个用户', { count: parsedRows.length })}
              </Button>
            )}
          </div>
        </div>
      }
    >
      {!result ? (
        <>
          <Banner
            type='info'
            description={t(
              '格式：每行一个用户，逗号分隔：用户名,密码,显示名,分组,额度(元)。密码 8-20 位；分组留空为 default；额度留空为 0。已存在的用户名会标记为「重复」，不影响其他行。',
            )}
            closeIcon={null}
            style={{ marginBottom: 12 }}
          />
          <Upload
            accept='.csv,.txt'
            limit={1}
            draggable
            dragMainText={t('点击或拖拽上传 CSV 文件')}
            dragSubText={t('也可以直接在下方粘贴 CSV 内容')}
            onFileChange={handleFile}
            style={{ marginBottom: 12 }}
          />
          <TextArea
            value={rawText}
            onChange={updateRawText}
            placeholder={TEMPLATE_HEADER + '\nzhangsan,Passw0rd123,张三,default,10'}
            autosize={{ minRows: 8, maxRows: 16 }}
            style={{ fontFamily: 'monospace', fontSize: 12 }}
          />
          {parsedRows.length > 0 && (
            <div style={{ marginTop: 12 }}>
              <Text type='tertiary' size='small'>
                {t('解析到 {{count}} 行（预览前 10 行）', {
                  count: parsedRows.length,
                })}
              </Text>
              <Table
                size='small'
                pagination={false}
                dataSource={parsedRows.slice(0, 10)}
                rowKey='row'
                columns={[
                  { title: '#', dataIndex: 'row', width: 40 },
                  { title: t('用户名'), dataIndex: 'username' },
                  { title: t('显示名'), dataIndex: 'display_name' },
                  {
                    title: t('分组'),
                    dataIndex: 'group',
                    render: (v) =>
                      groupOptions?.some((g) => g.value === v) || v === 'default' ? (
                        v
                      ) : (
                        <Tag color='red'>{v}</Tag>
                      ),
                  },
                  {
                    title: t('额度(元)'),
                    dataIndex: 'quota_cny',
                    render: (v) => v || '-',
                  },
                ]}
              />
            </div>
          )}
        </>
      ) : (
        <>
          <Banner
            type={result.error_count > 0 ? 'warning' : 'success'}
            description={t(
              '共 {{total}} 行：成功 {{s}}，重复 {{d}}，失败 {{e}}',
              {
                total: result.total,
                s: result.success_count,
                d: result.duplicate,
                e: result.error_count,
              },
            )}
            closeIcon={null}
            style={{ marginBottom: 12 }}
          />
          <Table
            size='small'
            pagination={result.results.length > 20 ? { pageSize: 20 } : false}
            dataSource={result.results}
            rowKey='row'
            columns={[
              { title: '#', dataIndex: 'row', width: 50 },
              { title: t('用户名'), dataIndex: 'username' },
              {
                title: t('结果'),
                dataIndex: 'status',
                width: 90,
                render: statusTag,
              },
              { title: t('说明'), dataIndex: 'message' },
            ]}
          />
        </>
      )}
    </Modal>
  );
};

export default ImportUsersModal;
