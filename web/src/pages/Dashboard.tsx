import React, { useEffect, useState } from 'react';
import { Card, Col, Row, Statistic, Typography, Spin, Empty } from 'antd';
import {
  ApiOutlined, TeamOutlined, ThunderboltOutlined,
  BarChartOutlined,
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
  if (!stats) return <Empty description="Failed to load stats" />;

  return (
    <div>
      <Typography.Title level={4}>Dashboard</Typography.Title>

      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Total Requests"
              value={stats.total_requests}
              prefix={<ApiOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Accounts"
              value={stats.accounts_count}
              prefix={<TeamOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Avg Latency"
              value={stats.avg_latency_ms}
              suffix="ms"
              prefix={<ThunderboltOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Models Used"
              value={stats.by_model.length}
              prefix={<BarChartOutlined />}
            />
          </Card>
        </Col>
      </Row>

      {stats.by_model.length > 0 && (
        <Card title="Requests by Model" style={{ marginTop: 24 }}>
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={stats.by_model}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="model" />
              <YAxis />
              <Tooltip />
              <Bar dataKey="count" fill="#1677ff" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </Card>
      )}

      {stats.by_account.length > 0 && (
        <Card title="Requests by Account" style={{ marginTop: 24 }}>
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={stats.by_account}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="api_key" />
              <YAxis />
              <Tooltip />
              <Bar dataKey="count" fill="#52c41a" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </Card>
      )}
    </div>
  );
};

export default Dashboard;
