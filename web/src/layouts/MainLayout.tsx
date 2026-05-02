import React, { useEffect, useState } from 'react';
import { Layout, Menu, Typography, theme, Tag } from 'antd';
import {
  DashboardOutlined,
  TeamOutlined,
  SettingOutlined,
  CheckCircleOutlined,
} from '@ant-design/icons';
import { useNavigate, useLocation, Outlet } from 'react-router-dom';
import useDocumentTitle from '../hooks/useDocumentTitle';
import { api } from '../api';

const { Header, Sider, Content } = Layout;
const { Text } = Typography;

const menuItems = [
  { key: '/dashboard', icon: <DashboardOutlined />, label: '数据概览' },
  { key: '/accounts', icon: <TeamOutlined />, label: '账号管理' },
  { key: '/settings', icon: <SettingOutlined />, label: '系统设置' },
];

const MainLayout: React.FC = () => {
  const [collapsed, setCollapsed] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();
  const { token } = theme.useToken();
  useDocumentTitle();

  const [healthStatus, setHealthStatus] = useState<'ok' | 'error'>('ok');
  const [accountCount, setAccountCount] = useState(0);

  useEffect(() => {
    api.getHealth().then((h) => {
      setHealthStatus(h.status === 'ok' ? 'ok' : 'error');
      setAccountCount(h.accounts);
    }).catch(() => setHealthStatus('error'));
  }, []);

  const selectedKey = location.pathname.startsWith('/accounts') ? '/accounts'
    : location.pathname.startsWith('/settings') ? '/settings'
    : '/dashboard';

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        collapsible
        collapsed={collapsed}
        onCollapse={setCollapsed}
        style={{ background: token.colorBgContainer }}
      >
        <div style={{
          height: 48,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          borderBottom: `1px solid ${token.colorBorderSecondary}`,
        }}>
          <img src="/favicon.ico" alt="JoyCode" style={{ width: 24, height: 24, marginRight: collapsed ? 0 : 8 }} />
          {!collapsed && <Text strong style={{ fontSize: 15 }}>JoyCode 代理</Text>}
        </div>
        <Menu
          mode="inline"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
        />
      </Sider>
      <Layout>
        <Header style={{
          padding: '0 24px',
          background: token.colorBgContainer,
          borderBottom: `1px solid ${token.colorBorderSecondary}`,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <Tag color={healthStatus === 'ok' ? 'success' : 'error'} icon={<CheckCircleOutlined />}>
              {healthStatus === 'ok' ? '服务正常' : '服务异常'}
            </Tag>
            <Text type="secondary">{accountCount} 个账号在线</Text>
          </div>
          <Text type="secondary" style={{ fontSize: 12 }}>
            代理地址：localhost:34891
          </Text>
        </Header>
        <Content style={{ margin: 24 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
};

export default MainLayout;
