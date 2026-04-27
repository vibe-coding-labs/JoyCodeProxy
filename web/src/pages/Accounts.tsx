import React, { useEffect, useState } from 'react';
import {
  Table, Button, Space, Modal, Form, Input, Switch,
  message, Popconfirm, Tag, Typography,
} from 'antd';
import {
  PlusOutlined, DeleteOutlined, StarOutlined,
  SafetyCertificateOutlined, ReloadOutlined,
} from '@ant-design/icons';
import { api } from '../api';
import type { Account } from '../api';

const Accounts: React.FC = () => {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [form] = Form.useForm();
  const [validating, setValidating] = useState<string | null>(null);

  const fetchAccounts = async () => {
    setLoading(true);
    try {
      const data = await api.listAccounts();
      setAccounts(data);
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchAccounts(); }, []);

  const handleAdd = async (values: { api_key: string; pt_key: string; user_id: string; is_default?: boolean }) => {
    try {
      await api.addAccount(values);
      message.success(`Account "${values.api_key}" added`);
      setModalOpen(false);
      form.resetFields();
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : String(e));
    }
  };

  const handleRemove = async (apiKey: string) => {
    try {
      await api.removeAccount(apiKey);
      message.success(`Account "${apiKey}" removed`);
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : String(e));
    }
  };

  const handleSetDefault = async (apiKey: string) => {
    try {
      await api.setDefault(apiKey);
      message.success(`Default account set to "${apiKey}"`);
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : String(e));
    }
  };

  const handleValidate = async (apiKey: string) => {
    setValidating(apiKey);
    try {
      const result = await api.validateAccount(apiKey);
      if (result.valid) {
        message.success(`Account "${apiKey}" is valid`);
      } else {
        message.error(`Account "${apiKey}" validation failed`);
      }
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : String(e));
    } finally {
      setValidating(null);
    }
  };

  const columns = [
    {
      title: 'API Key',
      dataIndex: 'api_key',
      key: 'api_key',
      render: (text: string) => <Typography.Text code>{text}</Typography.Text>,
    },
    {
      title: 'User ID',
      dataIndex: 'user_id',
      key: 'user_id',
    },
    {
      title: 'Default',
      dataIndex: 'is_default',
      key: 'is_default',
      render: (val: boolean) => val ? <Tag color="blue"><StarOutlined /> Default</Tag> : null,
    },
    {
      title: 'Actions',
      key: 'actions',
      render: (_: unknown, record: Account) => (
        <Space>
          {!record.is_default && (
            <Button size="small" onClick={() => handleSetDefault(record.api_key)}>
              <StarOutlined /> Set Default
            </Button>
          )}
          <Button
            size="small"
            onClick={() => handleValidate(record.api_key)}
            loading={validating === record.api_key}
          >
            <SafetyCertificateOutlined /> Validate
          </Button>
          <Popconfirm
            title={`Remove account "${record.api_key}"?`}
            onConfirm={() => handleRemove(record.api_key)}
          >
            <Button size="small" danger><DeleteOutlined /></Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>Account Management</Typography.Title>
        <Space>
          <Button onClick={fetchAccounts} icon={<ReloadOutlined />}>Refresh</Button>
          <Button type="primary" onClick={() => setModalOpen(true)} icon={<PlusOutlined />}>
            Add Account
          </Button>
        </Space>
      </div>

      <Table
        dataSource={accounts}
        columns={columns}
        rowKey="api_key"
        loading={loading}
        pagination={false}
      />

      <Modal
        title="Add Account"
        open={modalOpen}
        onCancel={() => { setModalOpen(false); form.resetFields(); }}
        onOk={() => form.submit()}
      >
        <Form form={form} layout="vertical" onFinish={handleAdd}>
          <Form.Item name="api_key" label="API Key" rules={[{ required: true, message: 'Required' }]}>
            <Input placeholder="e.g. my-key-1 (used by clients to route)" />
          </Form.Item>
          <Form.Item name="pt_key" label="JoyCode ptKey" rules={[{ required: true, message: 'Required' }]}>
            <Input.Password placeholder="JoyCode ptKey credential" />
          </Form.Item>
          <Form.Item name="user_id" label="JoyCode User ID" rules={[{ required: true, message: 'Required' }]}>
            <Input placeholder="e.g. user-12345" />
          </Form.Item>
          <Form.Item name="is_default" label="Set as default" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default Accounts;
