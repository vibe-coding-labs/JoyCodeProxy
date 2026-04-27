import React, { useState } from 'react';
import { Layout, Menu, Typography, theme } from 'antd';
import {
  DashboardOutlined,
  TeamOutlined,
  SettingOutlined,
  ApiOutlined,
} from '@ant-design/icons';
import { useNavigate, useLocation, Outlet } from 'react-router-dom';
import useDocumentTitle from '../hooks/useDocumentTitle';

const { Header, Sider, Content } = Layout;
const { Text } = Typography;

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: '数据概览' },
  { key: '/accounts', icon: <TeamOutlined />, label: '账号管理' },
  { key: '/settings', icon: <SettingOutlined />, label: '系统设置' },
];

const MainLayout: React.FC = () => {
  const [collapsed, setCollapsed] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();
  const { token } = theme.useToken();
  useDocumentTitle();

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
          <ApiOutlined style={{ fontSize: 20, marginRight: collapsed ? 0 : 8, color: '#1677ff' }} />
          {!collapsed && <Text strong style={{ fontSize: 15 }}>JoyCode 代理</Text>}
        </div>
        <Menu
          mode="inline"
          selectedKeys={[location.pathname.startsWith('/accounts') ? '/accounts' : location.pathname]}
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
          <Text type="secondary">JoyCode API 代理服务 — 管理控制台</Text>
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
