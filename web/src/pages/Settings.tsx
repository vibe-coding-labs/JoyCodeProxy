import React, { useEffect, useState } from 'react';
import {
  Card, Form, Input, Button, InputNumber, Select, Switch, message,
  Spin, Typography, Space, Divider, Alert, Tooltip, Row, Col,
} from 'antd';
import {
  SaveOutlined, ReloadOutlined, SettingOutlined,
  QuestionCircleOutlined, ApiOutlined, CloudServerOutlined,
  SafetyCertificateOutlined, ThunderboltOutlined,
} from '@ant-design/icons';
import { api } from '../api';
import type { Settings } from '../api';

const { Text, Title } = Typography;

interface FieldConfig {
  key: string;
  label: string;
  tooltip: string;
  placeholder: string;
  type: 'input' | 'number' | 'select' | 'switch';
  options?: { label: string; value: string }[];
  suffix?: string;
}

const FIELD_GROUPS = [
  {
    title: '网络配置',
    icon: <CloudServerOutlined />,
    fields: [
      {
        key: 'proxy_host',
        label: '代理监听地址',
        tooltip: '代理服务绑定的网络接口。0.0.0.0 表示监听所有网卡（允许外部访问），127.0.0.1 表示仅本机访问',
        placeholder: '0.0.0.0',
        type: 'input' as const,
      },
      {
        key: 'proxy_port',
        label: '代理监听端口',
        tooltip: '代理服务的 HTTP 端口号。Claude Code 的 ANTHROPIC_BASE_URL 需要指向此端口',
        placeholder: '34891',
        type: 'number' as const,
      },
      {
        key: 'api_base_url',
        label: 'JoyCode API 地址',
        tooltip: 'JoyCode 后端 API 的基础地址。通常不需要修改，除非使用私有部署的 JoyCode 服务',
        placeholder: 'https://joycode-api.jd.com',
        type: 'input' as const,
      },
    ],
  },
  {
    title: '模型配置',
    icon: <ApiOutlined />,
    fields: [
      {
        key: 'default_model',
        label: '默认模型',
        tooltip: '当客户端未指定模型时使用的 JoyCode 模型。下拉列表为系统预设支持的模型',
        placeholder: 'JoyAI-Code',
        type: 'select' as const,
        options: [
          { label: 'JoyAI-Code — 主力代码模型（推荐）', value: 'JoyAI-Code' },
          { label: 'GLM-5.1 — 智谱 GLM 5.1', value: 'GLM-5.1' },
          { label: 'GLM-5 — 智谱 GLM 5', value: 'GLM-5' },
          { label: 'GLM-4.7 — 智谱 GLM 4.7', value: 'GLM-4.7' },
          { label: 'Kimi-K2.6 — Moonshot Kimi K2.6', value: 'Kimi-K2.6' },
          { label: 'Kimi-K2.5 — Moonshot Kimi K2.5', value: 'Kimi-K2.5' },
          { label: 'MiniMax-M2.7 — MiniMax M2.7', value: 'MiniMax-M2.7' },
          { label: 'Doubao-Seed-2.0-pro — 豆包 Seed 2.0 Pro', value: 'Doubao-Seed-2.0-pro' },
        ],
      },
      {
        key: 'default_max_tokens',
        label: '默认最大输出 Token',
        tooltip: '当客户端请求中未指定 max_tokens 时使用的默认值。更大的值允许更长的回复，但消耗更多配额',
        placeholder: '8192',
        type: 'number' as const,
      },
    ],
  },
  {
    title: '连接优化',
    icon: <ThunderboltOutlined />,
    fields: [
      {
        key: 'request_timeout',
        label: '请求超时（秒）',
        tooltip: '与 JoyCode 后端通信的读取超时时间。流式对话可能需要较长时间，建议不低于 60 秒',
        placeholder: '120',
        type: 'number' as const,
        suffix: '秒',
      },
      {
        key: 'max_retries',
        label: '最大重试次数',
        tooltip: '请求失败时的自动重试次数。网络不稳定时可适当增加',
        placeholder: '3',
        type: 'number' as const,
      },
      {
        key: 'max_connections',
        label: '最大连接数',
        tooltip: '与 JoyCode 后端的最大并发 HTTP 连接数。多账号场景下可适当增加',
        placeholder: '20',
        type: 'number' as const,
      },
    ],
  },
  {
    title: '日志与安全',
    icon: <SafetyCertificateOutlined />,
    fields: [
      {
        key: 'log_level',
        label: '日志级别',
        tooltip: '控制日志输出的详细程度。debug 最详细（适合排错），error 最精简（适合生产环境）',
        placeholder: 'info',
        type: 'select' as const,
        options: [
          { label: 'Debug — 最详细，输出所有调试信息', value: 'debug' },
          { label: 'Info — 常规信息，记录关键操作', value: 'info' },
          { label: 'Warning — 仅警告和错误', value: 'warning' },
          { label: 'Error — 仅错误信息', value: 'error' },
        ],
      },
      {
        key: 'enable_request_logging',
        label: '启用请求日志',
        tooltip: '记录每个 API 请求的详细信息（模型、延迟、状态码）。关闭后「数据概览」页面将无数据',
        placeholder: 'true',
        type: 'switch' as const,
      },
      {
        key: 'log_retention_days',
        label: '日志保留天数',
        tooltip: '请求日志的自动清理周期。超过此天数的日志将被自动删除，0 表示永久保留',
        placeholder: '30',
        type: 'number' as const,
        suffix: '天',
      },
    ],
  },
];

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
      message.error(e instanceof Error ? e.message : '加载设置失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchSettings(); }, [form]);

  const handleSave = async (values: Settings) => {
    setSaving(true);
    try {
      await api.updateSettings(values);
      message.success('设置已保存，部分配置需重启代理服务后生效');
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '保存设置失败');
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;

  const renderField = (field: FieldConfig) => {
    const label = (
      <Space size={4}>
        {field.label}
        <Tooltip title={field.tooltip}><QuestionCircleOutlined style={{ color: '#999' }} /></Tooltip>
      </Space>
    );

    switch (field.type) {
      case 'number':
        return (
          <Form.Item key={field.key} name={field.key} label={label}>
            <InputNumber style={{ width: '100%' }} placeholder={field.placeholder} addonAfter={field.suffix} />
          </Form.Item>
        );
      case 'select':
        return (
          <Form.Item key={field.key} name={field.key} label={label}>
            <Select placeholder={field.placeholder} options={field.options} allowClear />
          </Form.Item>
        );
      case 'switch':
        return (
          <Form.Item key={field.key} name={field.key} valuePropName="checked" label={label}>
            <Switch />
          </Form.Item>
        );
      default:
        return (
          <Form.Item key={field.key} name={field.key} label={label}>
            <Input placeholder={field.placeholder} />
          </Form.Item>
        );
    }
  };

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Title level={4} style={{ margin: 0 }}>系统设置</Title>
        <Button onClick={fetchSettings} icon={<ReloadOutlined />}>刷新</Button>
      </div>

      <Alert
        type="info"
        showIcon
        icon={<SettingOutlined />}
        message="代理服务配置"
        description={
          <span>
            以下设置控制代理服务的行为。每个字段旁的 <QuestionCircleOutlined /> 图标提供详细说明。
            修改后点击「保存设置」，部分配置（如监听端口、模型映射）需
            <Text code>joycode-proxy serve</Text> 重启后生效。配置存储在
            <Text code>~/.joycode-proxy/proxy.db</Text>。
          </span>
        }
        style={{ marginBottom: 16 }}
      />

      <Form form={form} layout="vertical" onFinish={handleSave}>
        {FIELD_GROUPS.map((group) => (
          <Card
            key={group.title}
            title={<Space>{group.icon} {group.title}</Space>}
            style={{ marginBottom: 16 }}
          >
            <Row gutter={[24, 0]}>
              {group.fields.map((field) => (
                <Col xs={24} md={12} key={field.key}>
                  {renderField(field)}
                </Col>
              ))}
            </Row>
          </Card>
        ))}

        <Divider />

        <Space>
          <Button type="primary" htmlType="submit" loading={saving} icon={<SaveOutlined />} size="large">
            保存设置
          </Button>
          <Button onClick={fetchSettings} icon={<ReloadOutlined />}>恢复当前值</Button>
        </Space>
      </Form>
    </div>
  );
};

export default SettingsPage;
