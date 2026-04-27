import React, { useEffect, useState } from 'react';
import { Card, Col, Row, Statistic, Typography, Spin, Empty, Alert } from 'antd';
import {
  ApiOutlined, TeamOutlined, ThunderboltOutlined,
  BarChartOutlined, InfoCircleOutlined,
} from '@ant-design/icons';
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { api } from '../api';
import type { Stats } from '../api';

const Dashboard: React.FC = () => {
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchStats = async () => {
    setLoading(true);
    try {
      const data = await api.getStats();
      setStats(data);
    } catch (e: unknown) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchStats(); }, []);

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  if (!stats) return <Empty description="加载统计数据失败，请检查代理服务是否正常运行" />;

  return (
    <div>
      <Typography.Title level={4}>数据概览</Typography.Title>

      <Alert
        type="info"
        showIcon
        icon={<InfoCircleOutlined />}
        message="欢迎使用 JoyCode 代理服务"
        description="此处展示代理服务的运行状态和使用统计。您可以在「账号管理」中配置多个 JoyCode 账号，每个账号通过不同的 API Key 进行隔离路由。"
        style={{ marginBottom: 24 }}
      />

      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="总请求数"
              value={stats.total_requests}
              prefix={<ApiOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="已配置账号"
              value={stats.accounts_count}
              prefix={<TeamOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="平均响应延迟"
              value={stats.avg_latency_ms}
              suffix="毫秒"
              prefix={<ThunderboltOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="使用模型数"
              value={stats.by_model.length}
              prefix={<BarChartOutlined />}
            />
          </Card>
        </Col>
      </Row>

      {stats.by_model.length > 0 && (
        <Card title="各模型请求量" style={{ marginTop: 24 }}>
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={stats.by_model}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="model" />
              <YAxis />
              <Tooltip />
              <Bar dataKey="count" name="请求次数" fill="#1677ff" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </Card>
      )}

      {stats.by_account.length > 0 && (
        <Card title="各账号请求量" style={{ marginTop: 24 }}>
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={stats.by_account}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="api_key" />
              <YAxis />
              <Tooltip />
              <Bar dataKey="count" name="请求次数" fill="#52c41a" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </Card>
      )}
    </div>
  );
};

export default Dashboard;
