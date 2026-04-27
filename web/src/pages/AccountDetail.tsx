import React, { useEffect, useState } from 'react';
import {
  Card, Row, Col, Statistic, Typography, Spin, Tag, Select, Button,
  message, Tooltip, Space, Empty, Divider,
} from 'antd';
import {
  ArrowLeftOutlined, ApiOutlined, ThunderboltOutlined,
  CheckCircleOutlined, WarningOutlined, ReloadOutlined,
  QuestionCircleOutlined, UserOutlined, ClockCircleOutlined,
  SettingOutlined,
} from '@ant-design/icons';
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip as RTooltip, ResponsiveContainer } from 'recharts';
import { useParams, useNavigate } from 'react-router-dom';
import { api } from '../api';
import type { Account, AccountStats, ModelInfo } from '../api';

const BUILTIN_MODELS = [
  { label: 'JoyAI-Code（推荐）', value: 'JoyAI-Code' },
  { label: 'GLM-5.1', value: 'GLM-5.1' },
  { label: 'GLM-5', value: 'GLM-5' },
  { label: 'GLM-4.7', value: 'GLM-4.7' },
  { label: 'Kimi-K2.6', value: 'Kimi-K2.6' },
  { label: 'Kimi-K2.5', value: 'Kimi-K2.5' },
  { label: 'MiniMax-M2.7', value: 'MiniMax-M2.7' },
  { label: 'Doubao-Seed-2.0-pro', value: 'Doubao-Seed-2.0-pro' },
];

const AccountDetail: React.FC = () => {
  const { apiKey } = useParams<{ apiKey: string }>();
  const navigate = useNavigate();
  const [account, setAccount] = useState<Account | null>(null);
  const [stats, setStats] = useState<AccountStats | null>(null);
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [modelLoading, setModelLoading] = useState(false);
  const [savingModel, setSavingModel] = useState(false);

  const decodedKey = apiKey ? decodeURIComponent(apiKey) : '';

  const fetchData = async () => {
    setLoading(true);
    try {
      const [accounts, statsData] = await Promise.all([
        api.listAccounts(),
        api.getAccountStats(decodedKey),
      ]);
      const acc = accounts.find((a) => a.api_key === decodedKey);
      setAccount(acc || null);
      setStats(statsData);
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '加载账号详情失败');
    } finally {
      setLoading(false);
    }
  };

  const fetchModels = async () => {
    setModelLoading(true);
    try {
      const data = await api.listAccountModels(decodedKey);
      setModels(data);
    } catch {
      // Fallback to builtin models
    } finally {
      setModelLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, [decodedKey]);
  useEffect(() => { fetchModels(); }, [decodedKey]);

  const handleModelChange = async (newModel: string) => {
    setSavingModel(true);
    try {
      await api.updateAccountModel(decodedKey, newModel);
      message.success(`默认模型已更新为「${newModel || '未设置'}」`);
      fetchData();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '更新默认模型失败');
    } finally {
      setSavingModel(false);
    }
  };

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  if (!account) return <Empty description="账号不存在" />;

  const allModelOptions = [
    ...BUILTIN_MODELS,
    ...models
      .filter((m) => !BUILTIN_MODELS.some((b) => b.value === m.id))
      .map((m) => ({ label: m.name || m.id, value: m.id })),
  ];

  return (
    <div>
      {/* 顶部操作栏 */}
      <div style={{ marginBottom: 20, display: 'flex', alignItems: 'center', gap: 12 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/accounts')}>
          返回列表
        </Button>
        <Typography.Title level={4} style={{ margin: 0 }}>
          {decodedKey}
        </Typography.Title>
        {account.is_default && <Tag color="blue">默认账号</Tag>}
        <Button icon={<ReloadOutlined />} onClick={() => { fetchData(); fetchModels(); }} style={{ marginLeft: 'auto' }}>
          刷新
        </Button>
      </div>

      {/* 账号信息 + 模型配置 */}
      <Row gutter={[16, 16]}>
        <Col xs={24} md={12}>
          <Card title={<><UserOutlined /> 基本信息</>} size="small">
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                <Typography.Text type="secondary">路由密钥</Typography.Text>
                <Typography.Text code>{account.api_key}</Typography.Text>
              </div>
              <Divider style={{ margin: 0 }} />
              <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                <Typography.Text type="secondary">用户 ID</Typography.Text>
                <Typography.Text>{account.user_id}</Typography.Text>
              </div>
              <Divider style={{ margin: 0 }} />
              <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                <Typography.Text type="secondary">创建时间</Typography.Text>
                <Typography.Text>{account.created_at || '-'}</Typography.Text>
              </div>
            </div>
          </Card>
        </Col>
        <Col xs={24} md={12}>
          <Card title={<><SettingOutlined /> 模型配置</>} size="small">
            <div style={{ marginBottom: 12 }}>
              <Space size={4} style={{ marginBottom: 8 }}>
                <Typography.Text type="secondary">默认模型</Typography.Text>
                <Tooltip title="此账号的默认模型。当客户端未指定模型时使用。点击「获取在线模型」可从 JoyCode API 获取该账号支持的全部模型">
                  <QuestionCircleOutlined style={{ color: '#999' }} />
                </Tooltip>
              </Space>
            </div>
            <Space>
              <Select
                style={{ width: 260 }}
                value={account.default_model || undefined}
                placeholder="未设置 — 使用系统默认"
                options={allModelOptions}
                allowClear
                loading={modelLoading}
                onChange={handleModelChange}
                disabled={savingModel}
              />
              <Button size="small" onClick={fetchModels} loading={modelLoading}>
                获取在线模型
              </Button>
            </Space>
            {account.default_model && (
              <Typography.Text type="secondary" style={{ display: 'block', marginTop: 8, fontSize: 12 }}>
                客户端未指定模型时将自动使用「{account.default_model}」
              </Typography.Text>
            )}
          </Card>
        </Col>
      </Row>

      {/* 统计卡片 */}
      {stats && (
        <>
          <Typography.Title level={5} style={{ marginTop: 24, marginBottom: 12 }}>
            <ClockCircleOutlined /> 使用统计
          </Typography.Title>
          <Row gutter={[12, 12]} style={{ marginBottom: 20 }}>
            <Col xs={12} sm={6}>
              <Card size="small">
                <Statistic
                  title="总请求数"
                  value={stats.total_requests}
                  prefix={<ApiOutlined />}
                  valueStyle={{ fontSize: 22 }}
                />
              </Card>
            </Col>
            <Col xs={12} sm={6}>
              <Card size="small">
                <Statistic
                  title="平均延迟"
                  value={stats.avg_latency_ms}
                  suffix="ms"
                  prefix={<ThunderboltOutlined />}
                  valueStyle={{ fontSize: 22 }}
                />
              </Card>
            </Col>
            <Col xs={12} sm={6}>
              <Card size="small">
                <Statistic
                  title="流式请求"
                  value={stats.stream_count}
                  prefix={<CheckCircleOutlined />}
                  valueStyle={{ fontSize: 22 }}
                />
              </Card>
            </Col>
            <Col xs={12} sm={6}>
              <Card size="small">
                <Statistic
                  title="错误请求"
                  value={stats.error_count}
                  prefix={<WarningOutlined />}
                  valueStyle={{ fontSize: 22, color: stats.error_count > 0 ? '#ff4d4f' : undefined }}
                />
              </Card>
            </Col>
          </Row>

          {/* 模型使用分布图表 */}
          {stats.by_model.length > 0 && (
            <Card title="模型使用分布" size="small">
              <ResponsiveContainer width="100%" height={240}>
                <BarChart data={stats.by_model}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="model" tick={{ fontSize: 12 }} />
                  <YAxis />
                  <RTooltip />
                  <Bar dataKey="count" name="请求次数" fill="#1677ff" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </Card>
          )}
        </>
      )}
    </div>
  );
};

export default AccountDetail;
