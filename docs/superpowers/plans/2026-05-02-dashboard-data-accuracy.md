# Dashboard 数据准确性优化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 修复 Dashboard 数据概览页面的数据质量问题，使统计数据真实、准确、有用。

**Architecture:** 后端 GetStats() SQL 查询增加过滤条件（排除空模型、关联有效账号、增加时间窗口）→ 前端 Dashboard 增加时间范围选择器、改进统计卡片展示。数据从 store 层清洗后输出，前端不再显示垃圾数据。

**Tech Stack:** Go 1.22, SQLite, React 18, Ant Design 5, Recharts

**Risks:**
- GetStats() SQL 变更可能影响 AccountDetail 页面的 GetAccountStats() → 缓解：只改 GetStats()，不动 GetAccountStats()
- 时间窗口过滤需要 created_at 索引 → 缓解：SQLite 对小数据量性能足够，暂不加索引

---

### Task 1: 后端 GetStats 数据清洗

**Depends on:** None
**Files:**
- Modify: `pkg/store/store.go:514-556`（GetStats 方法）

- [ ] **Step 1: 修改 GetStats() — 过滤空模型、关联有效账号、增加 24h 时间窗口**

文件: `pkg/store/store.go:514-556`（替换整个 GetStats 方法）

```go
func (s *Store) GetStats() (*Stats, error) {
	stats := &Stats{}

	// 只统计最近 24 小时的数据
	timeFilter := "created_at >= datetime('now', '-24 hours')"

	err := s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE "+timeFilter).Scan(&stats.TotalRequests)
	if err != nil {
		slog.Error("store: get stats count failed", "error", err)
		return nil, err
	}

	s.db.QueryRow("SELECT COALESCE(AVG(latency_ms), 0) FROM request_logs WHERE "+timeFilter).Scan(&stats.AvgLatencyMs)

	s.db.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&stats.AccountsCount)

	// 过滤空模型名
	rows, err := s.db.Query("SELECT model, COUNT(*) as cnt FROM request_logs WHERE "+timeFilter+" AND model != '' GROUP BY model ORDER BY cnt DESC")
	if err != nil {
		slog.Error("store: get stats by model query failed", "error", err)
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mc ModelCount
		if err := rows.Scan(&mc.Model, &mc.Count); err != nil {
			return nil, err
		}
		stats.ByModel = append(stats.ByModel, mc)
	}

	// 只显示当前有效账号的请求量，将已删除账号归入 "other"
	validKeys := make(map[string]bool)
	accounts, _ := s.ListAccounts()
	for _, a := range accounts {
		validKeys[a.APIKey] = true
	}

	rows2, err := s.db.Query("SELECT api_key, COUNT(*) as cnt FROM request_logs WHERE "+timeFilter+" GROUP BY api_key ORDER BY cnt DESC")
	if err != nil {
		slog.Error("store: get stats by account query failed", "error", err)
		return nil, err
	}
	defer rows2.Close()
	otherCount := 0
	for rows2.Next() {
		var ac AccountCount
		if err := rows2.Scan(&ac.APIKey, &ac.Count); err != nil {
			return nil, err
		}
		if validKeys[ac.APIKey] {
			stats.ByAccount = append(stats.ByAccount, ac)
		} else {
			otherCount += ac.Count
		}
	}
	if otherCount > 0 {
		stats.ByAccount = append(stats.ByAccount, AccountCount{APIKey: "其他", Count: otherCount})
	}

	return stats, nil
}
```

- [ ] **Step 2: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 3: 提交**
Run: `git add pkg/store/store.go && git commit -m "fix(store): filter empty models and stale accounts in GetStats, add 24h time window"`

---

### Task 2: 前端 Dashboard 优化

**Depends on:** Task 1
**Files:**
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 1: 优化 Dashboard 组件 — 改进统计卡片、优化图表展示**

文件: `web/src/pages/Dashboard.tsx`（替换整个文件）

```tsx
import React, { useEffect, useState } from 'react';
import { Card, Col, Row, Statistic, Typography, Spin, Empty, Alert, Table, Tag } from 'antd';
import {
  ApiOutlined, TeamOutlined, ThunderboltOutlined,
  BarChartOutlined, InfoCircleOutlined, WarningOutlined,
  CheckCircleOutlined, ClockCircleOutlined,
} from '@ant-design/icons';
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { api } from '../api';
import type { Stats, RequestLog } from '../api';

const Dashboard: React.FC = () => {
  const [stats, setStats] = useState<Stats | null>(null);
  const [errors, setErrors] = useState<RequestLog[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchData = async () => {
    setLoading(true);
    try {
      const [statsData, errorsData] = await Promise.all([
        api.getStats(),
        api.getRecentErrors(10),
      ]);
      setStats(statsData);
      setErrors(errorsData.errors);
    } catch (e: unknown) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, []);

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  if (!stats) return <Empty description="加载统计数据失败，请检查代理服务是否正常运行" />;

  const errorRate = stats.total_requests > 0
    ? ((errors.length / stats.total_requests) * 100).toFixed(1)
    : '0.0';

  const errorColumns = [
    {
      title: '时间', dataIndex: 'created_at', key: 'time', width: 180,
      render: (v: string) => v ? new Date(v).toLocaleString('zh-CN') : '-',
    },
    { title: '端点', dataIndex: 'endpoint', key: 'endpoint', render: (v: string) => <code style={{ fontSize: 12 }}>{v}</code> },
    { title: '模型', dataIndex: 'model', key: 'model', render: (v: string) => v ? <Tag>{v}</Tag> : <Tag>-</Tag> },
    {
      title: '状态码', dataIndex: 'status_code', key: 'status',
      render: (v: number) => <span style={{ color: '#cf1322', fontWeight: 600 }}>{v}</span>,
    },
    { title: '延迟', dataIndex: 'latency_ms', key: 'latency', render: (v: number) => `${v}ms` },
  ];

  return (
    <div>
      <Typography.Title level={4}>数据概览</Typography.Title>

      <Alert
        type="info"
        showIcon
        icon={<InfoCircleOutlined />}
        message="最近 24 小时统计数据"
        description="以下展示代理服务最近 24 小时的运行状态和请求统计。"
        style={{ marginBottom: 24 }}
      />

      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="24h 请求数"
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
              suffix={<span style={{ fontSize: 14, color: '#999' }}>个</span>}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="平均响应延迟"
              value={Math.round(stats.avg_latency_ms)}
              suffix="ms"
              prefix={<ThunderboltOutlined />}
              valueStyle={{ color: stats.avg_latency_ms > 10000 ? '#cf1322' : stats.avg_latency_ms > 3000 ? '#faad14' : '#3f8600' }}
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

      {errors.length > 0 && (
        <Card
          title={<span><WarningOutlined style={{ color: '#faad14', marginRight: 8 }} />最近错误</span>}
          extra={<Tag color="red">{errorRate}% 错误率</Tag>}
          style={{ marginTop: 24 }}
        >
          <Table
            dataSource={errors}
            columns={errorColumns}
            rowKey="id"
            size="small"
            pagination={false}
          />
        </Card>
      )}

      {stats.by_model.length > 0 && (
        <Card
          title={<span><BarChartOutlined style={{ marginRight: 8 }} />各模型请求量（24h）</span>}
          style={{ marginTop: 24 }}
        >
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={stats.by_model}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="model" tick={{ fontSize: 12 }} />
              <YAxis />
              <Tooltip />
              <Bar dataKey="count" name="请求次数" fill="#1677ff" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </Card>
      )}

      {stats.by_account.length > 0 && (
        <Card
          title={<span><TeamOutlined style={{ marginRight: 8 }} />各账号请求量（24h）</span>}
          style={{ marginTop: 24 }}
        >
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={stats.by_account}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="api_key" tick={{ fontSize: 12 }} />
              <YAxis />
              <Tooltip />
              <Bar dataKey="count" name="请求次数" fill="#52c41a" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </Card>
      )}

      {stats.total_requests === 0 && (
        <Card style={{ marginTop: 24 }}>
          <Empty
            description="最近 24 小时暂无请求数据"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          >
            <Typography.Text type="secondary">
              配置好账号后，使用 Claude Code 或 Codex 连接到本代理即可看到统计数据
            </Typography.Text>
          </Empty>
        </Card>
      )}
    </div>
  );
};

export default Dashboard;
```

- [ ] **Step 2: 构建前端**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 3: 提交**
Run: `git add web/src/pages/Dashboard.tsx cmd/JoyCodeProxy/static/ && git commit -m "fix(web): improve dashboard data accuracy — 24h window, filter empty models, clean account display"`

---

### Task 3: 构建部署

**Depends on:** Task 2
**Files:** None (build only)

- [ ] **Step 1: 构建 Go 二进制**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 2: 重启服务**
Run: `launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist`
Expected:
  - Exit code: 0

- [ ] **Step 3: 验证 Dashboard API**
Run: `sleep 2 && curl -s http://localhost:34891/api/stats | python3 -m json.tool`
Expected:
  - by_model 不包含空字符串模型
  - by_account 只包含有效账号
  - total_requests 为 24h 内的请求数
