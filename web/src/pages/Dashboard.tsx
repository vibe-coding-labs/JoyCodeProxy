import React, { useEffect, useState } from 'react';
import { Card, Col, Row, Statistic, Spin, Empty, Typography, Progress } from 'antd';
import {
  ThunderboltOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  TeamOutlined,
  RiseOutlined,
  ApiOutlined,
} from '@ant-design/icons';
import { PieChart, Pie, Cell, BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { api } from '../api';
import type { Stats } from '../api';

const COLORS = ['#1677ff', '#52c41a', '#faad14', '#722ed1', '#eb2f96', '#13c2c2'];

const Dashboard: React.FC = () => {
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchData = async () => {
    setLoading(true);
    try {
      const data = await api.getStats();
      setStats(data);
    } catch (e) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, []);

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  if (!stats) return <Empty description="无法加载统计数据" />;

  const successRate = stats.total_requests > 0
    ? Math.round((stats.success_count / stats.total_requests) * 100)
    : 100;

  const modelPieData = stats.by_model.map((m) => ({ name: m.model, value: m.count }));
  const accountBarData = stats.by_account.map((a) => ({ name: a.api_key, value: a.count }));

  return (
    <div>
      {/* 顶部系统状态横幅 */}
      <Card
        style={{ marginBottom: 16, background: 'linear-gradient(135deg, #1677ff 0%, #0958d9 100%)', border: 'none', borderRadius: 12 }}
        bodyStyle={{ padding: '20px 24px' }}
      >
        <Row align="middle" justify="space-between">
          <Col>
            <Typography.Text style={{ color: 'rgba(255,255,255,0.85)', fontSize: 13 }}>
              JoyCode API 代理服务
            </Typography.Text>
            <Typography.Title level={3} style={{ color: '#fff', margin: '4px 0 0' }}>
              数据概览
            </Typography.Title>
          </Col>
          <Col>
            <Row gutter={24}>
              <Col style={{ textAlign: 'center' }}>
                <div style={{ color: 'rgba(255,255,255,0.7)', fontSize: 12 }}>24h 请求数</div>
                <div style={{ color: '#fff', fontSize: 24, fontWeight: 700 }}>{stats.total_requests}</div>
              </Col>
              <Col style={{ textAlign: 'center' }}>
                <div style={{ color: 'rgba(255,255,255,0.7)', fontSize: 12 }}>在线账号</div>
                <div style={{ color: '#fff', fontSize: 24, fontWeight: 700 }}>{stats.accounts_count}</div>
              </Col>
              <Col style={{ textAlign: 'center' }}>
                <div style={{ color: 'rgba(255,255,255,0.7)', fontSize: 12 }}>成功率</div>
                <div style={{ color: '#fff', fontSize: 24, fontWeight: 700 }}>{successRate}%</div>
              </Col>
            </Row>
          </Col>
        </Row>
      </Card>

      {/* 核心指标卡片 */}
      <Row gutter={[16, 16]}>
        <Col xs={12} sm={8} md={4}>
          <Card size="small" style={{ borderRadius: 8, textAlign: 'center' }}>
            <Statistic
              title={<span style={{ fontSize: 12 }}>成功请求</span>}
              value={stats.success_count}
              prefix={<CheckCircleOutlined style={{ color: '#52c41a' }} />}
              valueStyle={{ fontSize: 22, color: '#52c41a' }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card size="small" style={{ borderRadius: 8, textAlign: 'center' }}>
            <Statistic
              title={<span style={{ fontSize: 12 }}>失败请求</span>}
              value={stats.error_count}
              prefix={<CloseCircleOutlined style={{ color: '#cf1322' }} />}
              valueStyle={{ fontSize: 22, color: stats.error_count > 0 ? '#cf1322' : '#52c41a' }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card size="small" style={{ borderRadius: 8, textAlign: 'center' }}>
            <Statistic
              title={<span style={{ fontSize: 12 }}>平均延迟</span>}
              value={Math.round(stats.avg_latency_ms)}
              suffix="ms"
              prefix={<ThunderboltOutlined />}
              valueStyle={{ fontSize: 22 }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card size="small" style={{ borderRadius: 8, textAlign: 'center' }}>
            <Statistic
              title={<span style={{ fontSize: 12 }}>流式请求</span>}
              value={stats.stream_count}
              prefix={<ApiOutlined />}
              valueStyle={{ fontSize: 22 }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card size="small" style={{ borderRadius: 8, textAlign: 'center' }}>
            <Statistic
              title={<span style={{ fontSize: 12 }}>使用模型</span>}
              value={stats.by_model.length}
              prefix={<RiseOutlined />}
              valueStyle={{ fontSize: 22 }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card size="small" style={{ borderRadius: 8, textAlign: 'center' }}>
            <Statistic
              title={<span style={{ fontSize: 12 }}>配置账号</span>}
              value={stats.accounts_count}
              prefix={<TeamOutlined />}
              valueStyle={{ fontSize: 22 }}
            />
          </Card>
        </Col>
      </Row>

      {/* 成功率仪表盘 + 模型分布 + 账号分布 */}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} md={8}>
          <Card size="small" title="请求成功率" style={{ borderRadius: 8 }}>
            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
              <Progress
                type="dashboard"
                percent={successRate}
                strokeColor={successRate >= 95 ? '#52c41a' : successRate >= 80 ? '#faad14' : '#cf1322'}
                format={(p) => `${p}%`}
                size={140}
              />
              <div style={{ textAlign: 'center', marginTop: 8, color: '#666', fontSize: 12 }}>
                {stats.success_count} 成功 / {stats.error_count} 失败 / 共 {stats.total_requests}
              </div>
            </div>
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card size="small" title="模型分布" style={{ borderRadius: 8, minHeight: 260 }}>
            {modelPieData.length > 0 ? (
              <ResponsiveContainer width="100%" height={200}>
                <PieChart>
                  <Pie data={modelPieData} cx="50%" cy="50%" innerRadius={45} outerRadius={75} dataKey="value" label={({ name, percent }: { name?: string; percent?: number }) => `${name ?? ''} ${((percent ?? 0) * 100).toFixed(0)}%`} labelLine={false}>
                    {modelPieData.map((_, i) => <Cell key={i} fill={COLORS[i % COLORS.length]} />)}
                  </Pie>
                  <Tooltip />
                </PieChart>
              </ResponsiveContainer>
            ) : (
              <Empty description="暂无数据" image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card size="small" title="账号请求分布" style={{ borderRadius: 8, minHeight: 260 }}>
            {accountBarData.length > 0 ? (
              <ResponsiveContainer width="100%" height={200}>
                <BarChart data={accountBarData}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="name" tick={{ fontSize: 11 }} />
                  <YAxis tick={{ fontSize: 11 }} />
                  <Tooltip />
                  <Bar dataKey="value" name="请求次数" fill="#1677ff" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <Empty description="暂无数据" image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>
      </Row>

      {/* 空状态 */}
      {stats.total_requests === 0 && (
        <Card style={{ marginTop: 16, borderRadius: 8 }}>
          <Empty description="最近 24 小时暂无请求数据">
            <Typography.Text type="secondary">
              配置好账号后，使用 Claude Code 或 Codex 连接到本代理即可看到统计数据
            </Typography.Text>
          </Empty>
        </Card>
      )}
    </div>
  );
};

export default Dashboard;
