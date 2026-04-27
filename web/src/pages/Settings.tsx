import React, { useEffect, useState } from 'react';
import { Card, Form, Input, Button, message, Spin, Typography, Space, Divider } from 'antd';
import { SaveOutlined, ReloadOutlined } from '@ant-design/icons';
import { api } from '../api';
import type { Settings } from '../api';

const SettingsPage: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm();

  const fetchSettings = async () => {
    setLoading(true);
    try {
      const data = await api.getSettings();
      form.setFieldsValue(data);
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchSettings(); }, [form]);

  const handleSave = async (values: Settings) => {
    setSaving(true);
    try {
      await api.updateSettings(values);
      message.success('Settings saved');
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>Settings</Typography.Title>
        <Button onClick={fetchSettings} icon={<ReloadOutlined />}>Refresh</Button>
      </div>

      <Card>
        <Form form={form} layout="vertical" onFinish={handleSave}>
          <Typography.Text type="secondary">
            These settings are stored in SQLite at <Typography.Text code>~/.joycode-proxy/proxy.db</Typography.Text>
          </Typography.Text>

          <Divider />

          <Form.Item name="proxy_host" label="Proxy Host">
            <Input placeholder="0.0.0.0" />
          </Form.Item>
          <Form.Item name="proxy_port" label="Proxy Port">
            <Input placeholder="34891" />
          </Form.Item>
          <Form.Item name="default_model" label="Default Model">
            <Input placeholder="JoyAI-Code" />
          </Form.Item>
          <Form.Item name="log_level" label="Log Level">
            <Input placeholder="info" />
          </Form.Item>

          <Space>
            <Button type="primary" htmlType="submit" loading={saving} icon={<SaveOutlined />}>
              Save Settings
            </Button>
          </Space>
        </Form>
      </Card>
    </div>
  );
};

export default SettingsPage;
