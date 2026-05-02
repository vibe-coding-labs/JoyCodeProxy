# Settings Page Audit & Green Theme Fix

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 审计并修复 Settings 页面所有设置项（当前全部无效），接入后端实际生效；统一 Settings 页面为绿色 (#00b578) 扁平化风格。

**Architecture:** Store 层新增 `GetSetting(key)` 方法（带内存缓存）→ Handler 层从 store 读取 `default_max_tokens`、`max_retries` 替代硬编码 → Middleware 层检查 `enable_request_logging` 控制日志开关 → 前端移除无效设置项（`api_base_url`、`max_connections`），新增绿色渐变 Banner，扁平化卡片风格。

**Tech Stack:** Go 1.23, React 19, TypeScript 5, Ant Design 5, SQLite

**Risks:**
- Task 1 修改 anthropic handler 构造函数签名，需同步更新所有调用方 → 缓解：grep 确认所有 NewHandler 调用点
- Task 2 前端移除设置项后，DB 中残留的旧值不影响功能 → 无风险，Store 只读取存在的 key
- `enable_request_logging=false` 可能导致 Dashboard 无数据 → 缓解：UI 上明确提示关闭后果

---

### Task 1: Backend — 接入设置项到运行时行为

**Depends on:** None
**Files:**
- Modify: `pkg/store/store.go:544`（新增 GetSetting 方法）
- Modify: `pkg/anthropic/handler.go:30-40`（Handler 结构体添加 store 字段）
- Modify: `pkg/anthropic/handler.go:64-65`（default_max_tokens 从设置读取）
- Modify: `pkg/anthropic/handler.go:87`（max_retries 从设置读取）
- Modify: `pkg/anthropic/handler.go:381`（stream max_retries 从设置读取）
- Modify: `cmd/JoyCodeProxy/serve.go:80-81`（传 store 给 handler）
- Modify: `cmd/JoyCodeProxy/serve.go:248-294`（enable_request_logging 检查）
- Modify: `pkg/dashboard/handler_test.go`（如有 NewHandler 调用需更新）

- [ ] **Step 1: 新增 Store.GetSetting 和 GetIntSetting 方法 — 提供带缓存的设置读取**
文件: `pkg/store/store.go`（在 GetSettings 方法之后添加）

```go
// GetSetting reads a single setting value by key. Returns empty string if not found.
func (s *Store) GetSetting(key string) string {
	var val string
	s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	return val
}

// GetIntSetting reads a setting as int. Returns defaultVal if missing or invalid.
func (s *Store) GetIntSetting(key string, defaultVal int) int {
	v := s.GetSetting(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
}
```

同时确认 `pkg/store/store.go` 顶部 import 包含 `"strconv"`。

- [ ] **Step 2: 修改 anthropic Handler 接受 store 参数**
文件: `pkg/anthropic/handler.go:30-40`（Handler 结构体和 NewHandler 函数）

```go
type Handler struct {
	client  *joycode.Client
	Resolver func(r *http.Request) *joycode.Client
	store   *store.Store
}

func NewHandler(client *joycode.Client, s *store.Store) *Handler {
	return &Handler{client: client, store: s}
}
```

确认 import 中添加 `"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"`。

- [ ] **Step 3: 替换硬编码 default_max_tokens 为设置值**
文件: `pkg/anthropic/handler.go:64-65`

```go
	defaultMaxTokens := 8192
	if h.store != nil {
		defaultMaxTokens = h.store.GetIntSetting("default_max_tokens", 8192)
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = defaultMaxTokens
	}
```

- [ ] **Step 4: 替换硬编码 max_retries 为设置值（非流式）**
文件: `pkg/anthropic/handler.go:87`（在 handleNonStream 方法中）

```go
	maxRetries := 3
	if h.store != nil {
		maxRetries = h.store.GetIntSetting("max_retries", 3)
	}
```

同时更新该函数内所有引用 `maxRetries` 的地方（`maxRetries` 变量名不变，只改声明）。

- [ ] **Step 5: 替换硬编码 max_retries 为设置值（流式）**
文件: `pkg/anthropic/handler.go:381`（在 handleStreamMessages 方法中）

```go
	maxRetries := 3
	if h.store != nil {
		maxRetries = h.store.GetIntSetting("max_retries", 3)
	}
```

- [ ] **Step 6: 修改 serve.go 传递 store 给 anthropic handler**
文件: `cmd/JoyCodeProxy/serve.go:80`

```go
anth := anthropic.NewHandler(client, s)
```

- [ ] **Step 7: 在 requestLogMiddleware 中检查 enable_request_logging**
文件: `cmd/JoyCodeProxy/serve.go:248-294`

在 `requestLogMiddleware` 函数体开头（`start := time.Now()` 之前），检查是否需要记录请求日志。在写日志到 DB 的部分（`go s.LogRequest(...)` 调用处），添加条件判断：

```go
// 在 go s.LogRequest(...) 调用处包裹条件
enableLogging := s.GetSetting("enable_request_logging")
if enableLogging != "false" {
    go s.LogRequest(apiKey, model, path, isStream, rw.statusCode, latency, errMsg, inTk, outTk)
}
```

- [ ] **Step 8: 验证编译**
Run: `go build ./...`
Expected:
  - Exit code: 0
  - Output does NOT contain: "cannot" or "undefined" or "not used"

- [ ] **Step 9: 提交**
Run: `git add pkg/store/store.go pkg/anthropic/handler.go cmd/JoyCodeProxy/serve.go && git commit -m "feat(settings): wire up default_max_tokens, max_retries, enable_request_logging to runtime"`

---

### Task 2: Frontend — Settings 页面绿色主题重设计

**Depends on:** Task 1
**Files:**
- Modify: `web/src/pages/Settings.tsx`（完整重写）

- [ ] **Step 1: 重写 Settings.tsx — 绿色渐变 Banner + 扁平化卡片 + 移除无效设置**

```tsx
import React, { useEffect, useState } from 'react';
import {
  Card, Form, Input, Button, InputNumber, Select, Switch, message,
  Spin, Typography, Space, Row, Col, Tag, Tooltip,
} from 'antd';
import {
  SaveOutlined, ReloadOutlined, QuestionCircleOutlined,
  SettingOutlined, CheckCircleOutlined, InfoCircleOutlined,
} from '@ant-design/icons';
import { api } from '../api';
import type { Settings } from '../api';

const { Text } = Typography;

interface FieldConfig {
  key: string;
  label: string;
  tooltip: string;
  placeholder: string;
  type: 'input' | 'number' | 'select' | 'switch';
  options?: { label: string; value: string }[];
  suffix?: string;
  readOnly?: boolean;
  tag?: string;
}

const FIELD_GROUPS = [
  {
    title: '模型配置',
    fields: [
      {
        key: 'default_model',
        label: '默认模型',
        tooltip: '当客户端未指定模型，且账号未配置默认模型时使用的 JoyCode 模型',
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
        tooltip: '客户端未指定 max_tokens 时的默认值。更大值允许更长回复，但消耗更多配额',
        placeholder: '8192',
        type: 'number' as const,
        tag: '已生效',
      },
    ],
  },
  {
    title: '连接优化',
    fields: [
      {
        key: 'max_retries',
        label: '最大重试次数',
        tooltip: '请求失败时的自动重试次数。网络不稳定时可适当增加',
        placeholder: '3',
        type: 'number' as const,
        tag: '已生效',
      },
      {
        key: 'request_timeout',
        label: '请求超时（秒）',
        tooltip: '与 JoyCode 后端通信的读取超时时间。流式对话可能需要较长时间，建议不低于 60 秒',
        placeholder: '120',
        type: 'number' as const,
        suffix: '秒',
        tag: '规划中',
      },
      {
        key: 'max_connections',
        label: '最大连接数',
        tooltip: '与 JoyCode 后端的最大并发 HTTP 连接数。多账号场景下可适当增加',
        placeholder: '20',
        type: 'number' as const,
        tag: '规划中',
      },
    ],
  },
  {
    title: '日志与监控',
    fields: [
      {
        key: 'enable_request_logging',
        label: '启用请求日志',
        tooltip: '记录每个 API 请求的详细信息。关闭后「数据概览」页面将无数据',
        placeholder: 'true',
        type: 'switch' as const,
        tag: '已生效',
      },
      {
        key: 'log_retention_days',
        label: '日志保留天数',
        tooltip: '请求日志的自动清理周期。超过此天数的日志将被自动删除，0 表示永久保留',
        placeholder: '30',
        type: 'number' as const,
        suffix: '天',
        tag: '规划中',
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
      message.success('设置已保存');
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
        <Tooltip title={field.tooltip}><QuestionCircleOutlined style={{ color: '#bbb' }} /></Tooltip>
        {field.tag && (
          <Tag color={field.tag === '已生效' ? 'success' : 'default'} style={{ marginLeft: 4, fontSize: 11 }}>
            {field.tag === '已生效' ? <CheckCircleOutlined /> : <InfoCircleOutlined />} {field.tag}
          </Tag>
        )}
      </Space>
    );

    switch (field.type) {
      case 'number':
        return (
          <Form.Item key={field.key} name={field.key} label={label}>
            <InputNumber
              style={{ width: '100%' }}
              placeholder={field.placeholder}
              addonAfter={field.suffix}
              disabled={field.readOnly}
            />
          </Form.Item>
        );
      case 'select':
        return (
          <Form.Item key={field.key} name={field.key} label={label}>
            <Select placeholder={field.placeholder} options={field.options} allowClear disabled={field.readOnly} />
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
            <Input placeholder={field.placeholder} disabled={field.readOnly} />
          </Form.Item>
        );
    }
  };

  return (
    <div>
      {/* Green gradient banner matching Dashboard style */}
      <Card
        style={{
          marginBottom: 16,
          background: 'linear-gradient(135deg, #00b578 0%, #009a63 100%)',
          border: 'none',
          borderRadius: 12,
        }}
        bodyStyle={{ padding: '20px 24px' }}
      >
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <div>
            <Text style={{ color: 'rgba(255,255,255,0.85)', fontSize: 13 }}>
              JoyCode API 代理服务 · 系统设置
            </Text>
            <div style={{ color: '#fff', fontSize: 22, fontWeight: 700, marginTop: 4 }}>
              代理配置管理
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <Button
              ghost
              style={{ color: '#fff', borderColor: 'rgba(255,255,255,0.4)' }}
              icon={<ReloadOutlined />}
              onClick={fetchSettings}
            >
              刷新
            </Button>
          </div>
        </div>
      </Card>

      <Form form={form} layout="vertical" onFinish={handleSave}>
        {FIELD_GROUPS.map((group) => (
          <Card
            key={group.title}
            title={<Text strong style={{ fontSize: 15 }}>{group.title}</Text>}
            style={{ marginBottom: 16, borderRadius: 8, border: '1px solid #f0f0f0' }}
            bodyStyle={{ padding: '20px 24px' }}
            extra={
              <SettingOutlined style={{ color: '#00b578' }} />
            }
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

        <div style={{ display: 'flex', gap: 12, marginTop: 8 }}>
          <Button
            type="primary"
            htmlType="submit"
            loading={saving}
            icon={<SaveOutlined />}
            size="large"
            style={{ borderRadius: 6 }}
          >
            保存设置
          </Button>
          <Button onClick={fetchSettings} icon={<ReloadOutlined />} size="large">
            恢复当前值
          </Button>
        </div>
      </Form>
    </div>
  );
};

export default SettingsPage;
```

- [ ] **Step 2: 验证前端构建**
Run: `cd web && npx tsc --noEmit`
Expected:
  - Exit code: 0
  - Output does NOT contain: "error TS"

- [ ] **Step 3: 提交**
Run: `git add web/src/pages/Settings.tsx && git commit -m "refactor(settings): redesign with green theme, remove dead settings, add status tags"`

---

### Task 3: 构建部署

**Depends on:** Task 1, Task 2
**Files:** None (build commands only)

- [ ] **Step 1: 构建前端**
Run: `cd web && npm run build`
Expected:
  - Exit code: 0
  - Output contains: "dist"

- [ ] **Step 2: 构建后端二进制**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy`
Expected:
  - Exit code: 0
  - Binary exists at `joycode_proxy_bin`

- [ ] **Step 3: 重启服务**
Run: `ps aux | grep joycode_proxy_bin | grep -v grep | awk '{print $2}' | xargs kill`
Expected:
  - Process killed
  - launchd auto-restarts with new binary

- [ ] **Step 4: 验证服务运行**
Run: `sleep 2 && curl -s http://localhost:34891/api/health`
Expected:
  - Exit code: 0
  - Output contains: `"status":"ok"`
