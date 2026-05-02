# Dashboard 大屏重设计 + 路由独立化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 将 Dashboard 重设计为扁平化大屏概览页，只展示聚合统计数据（不展示列表），增加 /dashboard 独立路由，Header 展示真实数据。

**Architecture:** 后端 GetStats 扩展返回 error_count/stream_count/success_rate → 前端路由 `/` 重定向到 `/dashboard`，菜单 key 更新 → Dashboard 重写为大屏卡片布局，展示 6 个核心指标 + 模型分布环形图 + 账号分布柱状图 + 系统状态条。

**Tech Stack:** React 18, Ant Design 5, Recharts (PieChart/BarChart), react-router-dom 6, Go 1.22, SQLite

**Risks:**
- Dashboard.tsx 重写代码量 ~160 行，需确保 recharts 组件正确 → 缓解：复用现有 recharts 依赖
- 后端 Stats 结构体变更需前后端同步 → 缓解：先改后端再改前端，类型对齐

---

### Task 1: 后端 GetStats 扩展 — 增加错误率、流式请求、成功率

**Depends on:** None
**Files:**
- Modify: `pkg/store/store.go`（Stats struct + GetStats method）

- [ ] **Step 1: 扩展 Stats 结构体 — 添加 ErrorCount、StreamCount、SuccessCount 字段**

文件: `pkg/store/store.go`（找到 Stats struct 定义，替换整个结构体）

```go
type Stats struct {
	TotalRequests int            `json:"total_requests"`
	AccountsCount int            `json:"accounts_count"`
	AvgLatencyMs  float64        `json:"avg_latency_ms"`
	ErrorCount    int            `json:"error_count"`
	StreamCount   int            `json:"stream_count"`
	SuccessCount  int            `json:"success_count"`
	ByModel       []ModelCount   `json:"by_model"`
	ByAccount     []AccountCount `json:"by_account"`
}
```

- [ ] **Step 2: 修改 GetStats() — 增加错误数、流式数、成功数查询**

文件: `pkg/store/store.go`（替换 GetStats 方法，在 `s.db.QueryRow("SELECT COUNT(*) FROM accounts")` 之后添加三个新查询）

在 `s.db.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&stats.AccountsCount)` 之后，`rows, err := s.db.Query("SELECT model...` 之前，插入：

```go
	timeFilter := "created_at >= datetime('now', '-24 hours')"
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE "+timeFilter+" AND status_code >= 400").Scan(&stats.ErrorCount)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE "+timeFilter+" AND stream = 1").Scan(&stats.StreamCount)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE "+timeFilter+" AND status_code < 400").Scan(&stats.SuccessCount)
```

注意：同时需要将已有的 timeFilter 变量定义移到方法开头，替换掉当前散落在各处的 `timeFilter` 字符串。整个 GetStats 方法替换为：

```go
func (s *Store) GetStats() (*Stats, error) {
	stats := &Stats{}
	tf := "created_at >= datetime('now', '-24 hours')"

	err := s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE " + tf).Scan(&stats.TotalRequests)
	if err != nil {
		slog.Error("store: get stats count failed", "error", err)
		return nil, err
	}

	s.db.QueryRow("SELECT COALESCE(AVG(latency_ms), 0) FROM request_logs WHERE "+tf).Scan(&stats.AvgLatencyMs)
	s.db.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&stats.AccountsCount)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE "+tf+" AND status_code >= 400").Scan(&stats.ErrorCount)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE "+tf+" AND stream = 1").Scan(&stats.StreamCount)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE "+tf+" AND status_code < 400").Scan(&stats.SuccessCount)

	rows, err := s.db.Query("SELECT model, COUNT(*) as cnt FROM request_logs WHERE "+tf+" AND model != '' GROUP BY model ORDER BY cnt DESC")
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

	validKeys := make(map[string]bool)
	accounts, _ := s.ListAccounts()
	for _, a := range accounts {
		validKeys[a.APIKey] = true
	}

	rows2, err := s.db.Query("SELECT api_key, COUNT(*) as cnt FROM request_logs WHERE "+tf+" GROUP BY api_key ORDER BY cnt DESC")
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

- [ ] **Step 3: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 4: 提交**
Run: `git add pkg/store/store.go && git commit -m "feat(store): extend Stats with error_count, stream_count, success_count for dashboard"`

---

### Task 2: 前端路由独立化 — Dashboard 获得 /dashboard 路由

**Depends on:** None
**Files:**
- Modify: `web/src/App.tsx`
- Modify: `web/src/layouts/MainLayout.tsx`
- Modify: `web/src/api.ts`

- [ ] **Step 1: 更新 App.tsx 路由 — 添加 /dashboard 路由并将 / 重定向**

文件: `web/src/App.tsx`（替换整个 Routes 区域）

```tsx
import React, { Suspense, lazy } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { ConfigProvider, Spin } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import MainLayout from './layouts/MainLayout';

const Dashboard = lazy(() => import('./pages/Dashboard'));
const Accounts = lazy(() => import('./pages/Accounts'));
const AccountDetail = lazy(() => import('./pages/AccountDetail'));
const Settings = lazy(() => import('./pages/Settings'));

const pageLoading = <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;

const App: React.FC = () => (
  <ConfigProvider locale={zhCN} theme={{ token: { colorPrimary: '#1677ff' } }}>
    <BrowserRouter>
      <Routes>
        <Route element={<MainLayout />}>
          <Route path="/dashboard" element={<Suspense fallback={pageLoading}><Dashboard /></Suspense>} />
          <Route path="/accounts" element={<Suspense fallback={pageLoading}><Accounts /></Suspense>} />
          <Route path="/accounts/:apiKey" element={<Suspense fallback={pageLoading}><AccountDetail /></Suspense>} />
          <Route path="/settings" element={<Suspense fallback={pageLoading}><Settings /></Suspense>} />
          <Route path="/" element={<Navigate to="/dashboard" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  </ConfigProvider>
);

export default App;
```

- [ ] **Step 2: 更新 MainLayout.tsx — 菜单 key 更新为 /dashboard，Header 展示真实数据**

文件: `web/src/layouts/MainLayout.tsx`（替换整个文件）

```tsx
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
```

- [ ] **Step 3: 更新 api.ts Stats interface — 添加新字段**

文件: `web/src/api.ts:15-21`（替换 Stats interface）

```typescript
export interface Stats {
  total_requests: number;
  accounts_count: number;
  avg_latency_ms: number;
  error_count: number;
  stream_count: number;
  success_count: number;
  by_model: { model: string; count: number }[];
  by_account: { api_key: string; count: number }[];
}
```

- [ ] **Step 4: 验证前端编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npx tsc -b --noEmit 2>&1 | head -20`
Expected:
  - Exit code: 0 or no TS errors related to Dashboard/api

- [ ] **Step 5: 提交**
Run: `git add web/src/App.tsx web/src/layouts/MainLayout.tsx web/src/api.ts && git commit -m "feat(web): add /dashboard route, show real health status in header"`

---

### Task 3: Dashboard 大屏重设计 — 扁平化概览页

**Depends on:** Task 1, Task 2
**Files:**
- Modify: `web/src/pages/Dashboard.tsx`（完全重写）

- [ ] **Step 1: 重写 Dashboard.tsx — 大屏扁平化概览，只展示聚合数据**

文件: `web/src/pages/Dashboard.tsx`（替换整个文件）

```tsx
import React, { useEffect, useState } from 'react';
import { Card, Col, Row, Statistic, Spin, Empty, Typography, Progress, Badge } from 'antd';
import {
  ThunderboltOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  ApiOutlined,
  TeamOutlined,
  RiseOutlined,
  StreamOutlined,
} from '@ant-design/icons';
import { PieChart, Pie, Cell, BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { api } from '../api';
import type { Stats } from '../api';

const COLORS = ['#1677ff', '#52c41a', '#faad14', '#722ed1', '#eb2f96', '#13c2c2'];

const Dashboard: React.FC = () => {
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchData = async () => {
    setLoading(true);
    try {
      const data = await api.getStats();
      setStats(data);
    } catch (e) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, []);

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  if (!stats) return <Empty description="无法加载统计数据" />;

  const successRate = stats.total_requests > 0
    ? Math.round((stats.success_count / stats.total_requests) * 100)
    : 100;
  const errorRate = stats.total_requests > 0
    ? Math.round((stats.error_count / stats.total_requests) * 100)
    : 0;
  const streamRate = stats.total_requests > 0
    ? Math.round((stats.stream_count / stats.total_requests) * 100)
    : 0;

  const modelPieData = stats.by_model.map((m) => ({ name: m.model, value: m.count }));
  const accountBarData = stats.by_account.map((a) => ({ name: a.api_key, value: a.count }));

  return (
    <div>
      {/* 顶部系统状态横幅 */}
      <Card
        style={{ marginBottom: 16, background: 'linear-gradient(135deg, #1677ff 0%, #0958d9 100%)', border: 'none', borderRadius: 12 }}
        bodyStyle={{ padding: '20px 24px' }}
      >
        <Row align="middle" justify="space-between">
          <Col>
            <Typography.Text style={{ color: 'rgba(255,255,255,0.85)', fontSize: 13 }}>
              JoyCode API 代理服务
            </Typography.Text>
            <Typography.Title level={3} style={{ color: '#fff', margin: '4px 0 0' }}>
              数据概览
            </Typography.Title>
          </Col>
          <Col>
            <Row gutter={24}>
              <Col style={{ textAlign: 'center' }}>
                <div style={{ color: 'rgba(255,255,255,0.7)', fontSize: 12 }}>24h 请求数</div>
                <div style={{ color: '#fff', fontSize: 24, fontWeight: 700 }}>{stats.total_requests}</div>
              </Col>
              <Col style={{ textAlign: 'center' }}>
                <div style={{ color: 'rgba(255,255,255,0.7)', fontSize: 12 }}>在线账号</div>
                <div style={{ color: '#fff', fontSize: 24, fontWeight: 700 }}>{stats.accounts_count}</div>
              </Col>
              <Col style={{ textAlign: 'center' }}>
                <div style={{ color: 'rgba(255,255,255,0.7)', fontSize: 12 }}>成功率</div>
                <div style={{ color: '#fff', fontSize: 24, fontWeight: 700 }}>{successRate}%</div>
              </Col>
            </Row>
          </Col>
        </Row>
      </Card>

      {/* 核心指标卡片 */}
      <Row gutter={[16, 16]}>
        <Col xs={12} sm={8} md={4}>
          <Card size="small" style={{ borderRadius: 8, textAlign: 'center' }}>
            <Statistic
              title={<span style={{ fontSize: 12 }}>成功请求</span>}
              value={stats.success_count}
              prefix={<CheckCircleOutlined style={{ color: '#52c41a' }} />}
              valueStyle={{ fontSize: 22, color: '#52c41a' }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card size="small" style={{ borderRadius: 8, textAlign: 'center' }}>
            <Statistic
              title={<span style={{ fontSize: 12 }}>失败请求</span>}
              value={stats.error_count}
              prefix={<CloseCircleOutlined style={{ color: '#cf1322' }} />}
              valueStyle={{ fontSize: 22, color: stats.error_count > 0 ? '#cf1322' : '#52c41a' }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card size="small" style={{ borderRadius: 8, textAlign: 'center' }}>
            <Statistic
              title={<span style={{ fontSize: 12 }}>平均延迟</span>}
              value={Math.round(stats.avg_latency_ms)}
              suffix="ms"
              prefix={<ThunderboltOutlined />}
              valueStyle={{ fontSize: 22 }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card size="small" style={{ borderRadius: 8, textAlign: 'center' }}>
            <Statistic
              title={<span style={{ fontSize: 12 }}>流式请求</span>}
              value={stats.stream_count}
              prefix={<StreamOutlined />}
              valueStyle={{ fontSize: 22 }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card size="small" style={{ borderRadius: 8, textAlign: 'center' }}>
            <Statistic
              title={<span style={{ fontSize: 12 }}>使用模型</span>}
              value={stats.by_model.length}
              prefix={<RiseOutlined />}
              valueStyle={{ fontSize: 22 }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card size="small" style={{ borderRadius: 8, textAlign: 'center' }}>
            <Statistic
              title={<span style={{ fontSize: 12 }}>配置账号</span>}
              value={stats.accounts_count}
              prefix={<TeamOutlined />}
              valueStyle={{ fontSize: 22 }}
            />
          </Card>
        </Col>
      </Row>

      {/* 成功率进度条 + 错误率 */}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} md={8}>
          <Card size="small" title="请求成功率" style={{ borderRadius: 8 }}>
            <Progress
              type="dashboard"
              percent={successRate}
              strokeColor={successRate >= 95 ? '#52c41a' : successRate >= 80 ? '#faad14' : '#cf1322'}
              format={(p) => `${p}%`}
              size={120}
            />
            <div style={{ textAlign: 'center', marginTop: 8, color: '#666', fontSize: 12 }}>
              {stats.success_count} 成功 / {stats.error_count} 失败 / 共 {stats.total_requests}
            </div>
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card size="small" title="模型分布" style={{ borderRadius: 8, minHeight: 240 }}>
            {modelPieData.length > 0 ? (
              <ResponsiveContainer width="100%" height={180}>
                <PieChart>
                  <Pie data={modelPieData} cx="50%" cy="50%" innerRadius={40} outerRadius={70} dataKey="value" label={({ name, percent }) => `${name} ${(percent * 100).toFixed(0)}%`} labelLine={false}>
                    {modelPieData.map((_, i) => <Cell key={i} fill={COLORS[i % COLORS.length]} />)}
                  </Pie>
                  <Tooltip />
                </PieChart>
              </ResponsiveContainer>
            ) : (
              <Empty description="暂无数据" image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card size="small" title="账号请求分布" style={{ borderRadius: 8, minHeight: 240 }}>
            {accountBarData.length > 0 ? (
              <ResponsiveContainer width="100%" height={180}>
                <BarChart data={accountBarData}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="name" tick={{ fontSize: 11 }} />
                  <YAxis tick={{ fontSize: 11 }} />
                  <Tooltip />
                  <Bar dataKey="value" name="请求次数" fill="#1677ff" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <Empty description="暂无数据" image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>
      </Row>

      {/* 空状态 */}
      {stats.total_requests === 0 && (
        <Card style={{ marginTop: 16, borderRadius: 8 }}>
          <Empty description="最近 24 小时暂无请求数据">
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
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build 2>&1 | tail -5`
Expected:
  - Exit code: 0
  - Output does NOT contain: "error" (warning about chunk size is ok)

- [ ] **Step 3: 提交**
Run: `git add web/src/pages/Dashboard.tsx cmd/JoyCodeProxy/static/ && git commit -m "feat(web): redesign dashboard as flat overview with real aggregated data"`

---

### Task 4: 构建部署

**Depends on:** Task 1, Task 2, Task 3
**Files:** None (build only)

- [ ] **Step 1: 构建 Go 二进制**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 2: 重启服务**
Run: `launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist`
Expected:
  - Exit code: 0

- [ ] **Step 3: 验证 Stats API**
Run: `sleep 2 && curl -s http://localhost:34891/api/stats | python3 -m json.tool`
Expected:
  - Output contains: "error_count", "stream_count", "success_count"
  - by_model 不包含空模型

- [ ] **Step 4: 验证路由跳转**
Run: `curl -s -o /dev/null -w "%{http_code} %{redirect_url}" http://localhost:34891/`
Expected:
  - HTTP 200 (SPA fallback serves index.html, JS handles redirect to /dashboard)
