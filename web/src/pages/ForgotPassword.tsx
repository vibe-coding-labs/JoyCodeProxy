import React, { useEffect, useState } from 'react';
import { Button, Card, Typography, Alert } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { Link } from 'react-router-dom';
import { authApi } from '../api';

const { Title, Text, Paragraph } = Typography;

const ForgotPasswordPage: React.FC = () => {
  const [exePath, setExePath] = useState('./joycode_proxy_bin');

  useEffect(() => {
    authApi.status().then((res) => {
      if (res.exe_path) {
        setExePath(res.exe_path);
      }
    }).catch(() => {});
  }, []);

  return (
    <div style={{
      minHeight: '100vh',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
    }}>
      <Card
        style={{ width: 520, borderRadius: 12, boxShadow: '0 8px 24px rgba(0,0,0,0.12)' }}
        styles={{ body: { padding: 32 } }}
      >
        <Title level={3} style={{ marginBottom: 8 }}>忘记密码</Title>
        <Paragraph type="secondary" style={{ marginBottom: 24 }}>
          Dashboard 的 root 密码需要通过服务器命令行重置。
        </Paragraph>

        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 20 }}
          message="在服务器终端执行以下命令"
        />

        <Text strong>交互式重置（会提示你输入新密码）：</Text>
        <pre style={{
          background: '#f5f5f5',
          padding: '12px 16px',
          borderRadius: 6,
          marginTop: 8,
          marginBottom: 20,
          fontSize: 13,
          overflow: 'auto',
        }}>
          {`${exePath} reset-password`}
        </pre>

        <Text strong>直接指定新密码：</Text>
        <pre style={{
          background: '#f5f5f5',
          padding: '12px 16px',
          borderRadius: 6,
          marginTop: 8,
          marginBottom: 20,
          fontSize: 13,
          overflow: 'auto',
        }}>
          {`${exePath} reset-password -p 你的新密码`}
        </pre>

        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 24 }}
          message={
            <span>
              密码至少 <Text strong>6 位</Text>，以 bcrypt 哈希加密存储在 SQLite 数据库中。
              重置后所有已登录的会话需要重新登录。
            </span>
          }
        />

        <Link to="/login">
          <Button icon={<ArrowLeftOutlined />} type="primary" ghost style={{ borderRadius: 6 }}>
            返回登录
          </Button>
        </Link>
      </Card>
    </div>
  );
};

export default ForgotPasswordPage;
