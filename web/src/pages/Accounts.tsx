import React, { useEffect, useState } from 'react';
import {
  Table, Button, Space, Modal, Form, Input, Switch, Select,
  message, Popconfirm, Tag, Typography, Alert, Tooltip,
} from 'antd';
import {
  PlusOutlined, DeleteOutlined, StarOutlined,
  SafetyCertificateOutlined, ReloadOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons';
import SvgClaudeCode from '../components/ClaudeCodeIcon';
import SvgCodex from '../components/CodexIcon';
import { useNavigate } from 'react-router-dom';
import { api } from '../api';
import type { Account } from '../api';

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

const getBaseURL = () => `http://${window.location.host}`;

const claudeCodeCmd = (apiKey: string, model = 'GLM-5.1') => [
  `API_TIMEOUT_MS=6000000 \\`,
  `CLAUDE_CODE_MAX_RETRIES=1000000 \\`,
  `ANTHROPIC_BASE_URL=${getBaseURL()} \\`,
  `ANTHROPIC_API_KEY="${apiKey}" \\`,
  `CLAUDE_CODE_MAX_OUTPUT_TOKENS=6553655 \\`,
  `ANTHROPIC_MODEL=${model} \\`,
  `claude --dangerously-skip-permissions`,
].join('\n');

const codexCmd = (apiKey: string, model = 'GLM-5.1') => [
  `OPENAI_BASE_URL=${getBaseURL()}/v1 \\`,
  `OPENAI_API_KEY="${apiKey}" \\`,
  `OPENAI_MODEL=${model} \\`,
  `codex`,
].join('\n');

const copyToClipboard = async (text: string, label: string) => {
  try {
    await navigator.clipboard.writeText(text);
    message.success(`${label} 命令已复制`);
  } catch {
    message.error('复制失败');
  }
};

const Accounts: React.FC = () => {
  const navigate = useNavigate();
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [form] = Form.useForm();
  const [validating, setValidating] = useState<string | null>(null);
  const [autoLogging, setAutoLogging] = useState(false);

  const fetchAccounts = async () => {
    setLoading(true);
    try {
      const data = await api.listAccounts();
      setAccounts(data);
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '获取账号列表失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchAccounts(); }, []);

  const handleAdd = async (values: { api_key: string; pt_key: string; user_id: string; is_default?: boolean; default_model?: string }) => {
    try {
      await api.addAccount(values);
      message.success(`账号「${values.api_key}」添加成功`);
      setModalOpen(false);
      form.resetFields();
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '添加账号失败');
    }
  };

  const handleAutoLogin = async () => {
    setAutoLogging(true);
    try {
      const result = await api.autoLogin();
      message.success(`一键登录成功！账号「${result.api_key}」已添加`);
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '一键登录失败');
    } finally {
      setAutoLogging(false);
    }
  };

  const handleRemove = async (apiKey: string) => {
    try {
      await api.removeAccount(apiKey);
      message.success(`账号「${apiKey}」已删除`);
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '删除账号失败');
    }
  };

  const handleSetDefault = async (apiKey: string) => {
    try {
      await api.setDefault(apiKey);
      message.success(`已将「${apiKey}」设为默认账号`);
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '设置默认账号失败');
    }
  };

  const handleRenewToken = async (apiKey: string) => {
    try {
      await api.renewToken(apiKey);
      message.success('API Token 已更新');
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '更新 Token 失败');
    }
  };

  const handleValidate = async (apiKey: string) => {
    setValidating(apiKey);
    try {
      const result = await api.validateAccount(apiKey);
      if (result.valid) {
        message.success(`账号「${apiKey}」验证通过，凭证有效`);
      } else {
        message.error(`账号「${apiKey}」验证失败，凭证无效或已过期`);
      }
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '验证请求失败');
    } finally {
      setValidating(null);
    }
  };

  const columns = [
    {
      title: '账户名',
      dataIndex: 'api_key',
      key: 'api_key',
      render: (text: string) => <Typography.Text strong>{text}</Typography.Text>,
    },
    {
      title: 'API Token',
      dataIndex: 'api_token',
      key: 'api_token',
      render: (token: string) => (
        <Typography.Text code copyable style={{ fontSize: 12 }}>
          {token.slice(0, 12)}...{token.slice(-4)}
        </Typography.Text>
      ),
    },
    {
      title: '用户 ID',
      dataIndex: 'user_id',
      key: 'user_id',
    },
    {
      title: '状态',
      dataIndex: 'is_default',
      key: 'is_default',
      render: (val: boolean) => val ? <Tag color="blue"><StarOutlined /> 默认账号</Tag> : null,
    },
    {
      title: '默认模型',
      dataIndex: 'default_model',
      key: 'default_model',
      render: (val: string) => val ? <Tag color="green">{val}</Tag> : <Typography.Text type="secondary">未设置</Typography.Text>,
    },
    {
      title: '快速启动',
      key: 'quickstart',
      width: 90,
      render: (_: unknown, record: Account) => (
        <Space size={4}>
          <Tooltip title={`复制 Claude Code 命令`}>
            <Button
              type="text"
              size="small"
              icon={<SvgClaudeCode />}
              onClick={(e) => { e.stopPropagation(); copyToClipboard(claudeCodeCmd(record.api_token, record.default_model || undefined), 'Claude Code'); }}
            />
          </Tooltip>
          <Tooltip title={`复制 Codex 命令`}>
            <Button
              type="text"
              size="small"
              icon={<SvgCodex />}
              onClick={(e) => { e.stopPropagation(); copyToClipboard(codexCmd(record.api_token, record.default_model || undefined), 'Codex'); }}
            />
          </Tooltip>
        </Space>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_: unknown, record: Account) => (
        <Space>
          {!record.is_default && (
            <Button size="small" onClick={() => handleSetDefault(record.api_key)}>
              <StarOutlined /> 设为默认
            </Button>
          )}
          <Popconfirm
            title="确定要重置 API Token 吗？"
            description="重置后旧 Token 将立即失效"
            onConfirm={() => handleRenewToken(record.api_key)}
          >
            <Button size="small">重置 Token</Button>
          </Popconfirm>
          <Button
            size="small"
            onClick={() => handleValidate(record.api_key)}
            loading={validating === record.api_key}
          >
            <SafetyCertificateOutlined /> 验证
          </Button>
          <Popconfirm
            title={`确定要删除账号「${record.api_key}」吗？`}
            description="删除后使用该密钥的客户端将无法访问"
            onConfirm={() => handleRemove(record.api_key)}
          >
            <Button size="small" danger><DeleteOutlined /> 删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>账号管理</Typography.Title>
        <Space>
          <Button onClick={fetchAccounts} icon={<ReloadOutlined />}>刷新</Button>
          <Button
            type="primary"
            onClick={handleAutoLogin}
            loading={autoLogging}
            icon={<SafetyCertificateOutlined />}
          >
            一键登录
          </Button>
          <Button onClick={() => setModalOpen(true)} icon={<PlusOutlined />}>
            手动添加
          </Button>
        </Space>
      </div>

      <Alert
        type="info"
        showIcon
        message="多账号路由说明"
        description="每个账号对应一个 JoyCode 后端凭证。客户端通过「路由密钥」(API Key) 来指定使用哪个账号。同一个密钥始终路由到同一个账号，以最大化缓存命中率。配置 Claude Code 时，将路由密钥填入 ANTHROPIC_API_KEY 环境变量即可。"
        style={{ marginBottom: 16 }}
      />

      <Table
        dataSource={accounts}
        columns={columns}
        rowKey="api_key"
        loading={loading}
        pagination={false}
        onRow={(record) => ({
          onClick: () => navigate(`/accounts/${encodeURIComponent(record.api_key)}`),
          style: { cursor: 'pointer' },
        })}
        locale={{ emptyText: '暂无账号，请点击「添加账号」按钮配置您的第一个 JoyCode 账号' }}
      />

      <Modal
        title="手动添加 JoyCode 账号"
        open={modalOpen}
        onCancel={() => { setModalOpen(false); form.resetFields(); }}
        onOk={() => form.submit()}
        okText="添加"
        cancelText="取消"
        width={560}
      >
        <Alert
          type="info"
          showIcon
          message="手动添加账号"
          description="填写 JoyCode 客户端凭证信息。推荐使用「一键登录」自动导入，此处适合手动配置多个账号。"
          style={{ marginBottom: 16 }}
        />
        <Form form={form} layout="vertical" onFinish={handleAdd}>
          <Form.Item
            name="api_key"
            label={
              <Space size={4}>
                路由密钥 (API Key)
                <Tooltip title="客户端使用此密钥来路由到对应的 JoyCode 账号。配置 Claude Code 时，将此值填入 ANTHROPIC_API_KEY 环境变量。建议使用易辨识的名称">
                  <QuestionCircleOutlined style={{ color: '#999' }} />
                </Tooltip>
              </Space>
            }
            rules={[{ required: true, message: '请输入路由密钥' }]}
          >
            <Input placeholder="例如：team-a、user-zhangsan、dev-key-01" />
          </Form.Item>
          <Form.Item
            name="pt_key"
            label={
              <Space size={4}>
                JoyCode ptKey 凭证
                <Tooltip title="从 JoyCode 客户端获取的 ptKey，用于后端 API 认证。获取方式：打开 JoyCode 桌面客户端 → 设置 → 开发者 → 复制 ptKey。凭证将以加密形式存储在本地数据库中">
                  <QuestionCircleOutlined style={{ color: '#999' }} />
                </Tooltip>
              </Space>
            }
            rules={[{ required: true, message: '请输入 ptKey' }]}
          >
            <Input.Password placeholder="粘贴从 JoyCode 客户端复制的 ptKey，例如：eyJhbGci..." />
          </Form.Item>
          <Form.Item
            name="user_id"
            label={
              <Space size={4}>
                JoyCode 用户 ID
                <Tooltip title="与 ptKey 对应的用户 ID。获取方式：打开 JoyCode 桌面客户端 → 设置 → 个人信息 → 复制用户 ID">
                  <QuestionCircleOutlined style={{ color: '#999' }} />
                </Tooltip>
              </Space>
            }
            rules={[{ required: true, message: '请输入用户 ID' }]}
          >
            <Input placeholder="例如：user-12345 或从 JoyCode 客户端复制" />
          </Form.Item>
          <Form.Item
            name="default_model"
            label={
              <Space size={4}>
                默认模型
                <Tooltip title="此账号使用的默认模型。留空则使用系统全局默认模型。添加账号后，可在账号列表中实时获取该账号支持的全部模型">
                  <QuestionCircleOutlined style={{ color: '#999' }} />
                </Tooltip>
              </Space>
            }
          >
            <Select
              placeholder="留空使用系统默认模型"
              options={BUILTIN_MODELS}
              allowClear
            />
          </Form.Item>
          <Form.Item
            name="is_default"
            valuePropName="checked"
            label={
              <Space size={4}>
                设为默认账号
                <Tooltip title="当客户端未提供路由密钥时，请求将自动路由到此默认账号。建议将最常用的账号设为默认">
                  <QuestionCircleOutlined style={{ color: '#999' }} />
                </Tooltip>
              </Space>
            }
          >
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default Accounts;
