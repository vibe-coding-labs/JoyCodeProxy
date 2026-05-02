# Account Stats 24h Dimension Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 在账户详情页的请求和 Token 统计中，增加「全部时间」维度，与现有 24 小时数据并列展示，让用户能一眼看到短期和长期用量。

**Architecture:** 后端 `GetAccountStats()` 已返回 24h 数据，新增 `all_time` 字段查询无时间过滤的全量汇总。前端 Stats 行每个卡片分两行展示：大字显示 24h 数据，小字标注全部时间数据。

**Tech Stack:** Go 1.22, SQLite 3, React 18, Ant Design 5, TypeScript 5

**Risks:**
- 全时间统计在大数据量下可能较慢 → 缓解：SQLite 的 COUNT/SUM 对有索引的查询足够快，request_logs 表数据量有限（自动清理）

---

### Task 1: Backend — Add all-time totals to account stats API

**Depends on:** None
**Files:**
- Modify: `pkg/store/store.go:69-79`（AccountStats 结构体）
- Modify: `pkg/store/store.go:711-749`（GetAccountStats 方法）

- [ ] **Step 1: 扩展 AccountStats 结构体 — 添加 AllTime 字段**
文件: `pkg/store/store.go:69-79`

```go
type AccountStats struct {
	APIKey        string          `json:"api_key"`
	TotalRequests int             `json:"total_requests"`
	TotalInputTk  int             `json:"total_input_tokens"`
	TotalOutputTk int             `json:"total_output_tokens"`
	ByModel       []ModelCount    `json:"by_model"`
	ByEndpoint    []EndpointCount `json:"by_endpoint"`
	AvgLatencyMs  float64         `json:"avg_latency_ms"`
	StreamCount   int             `json:"stream_count"`
	ErrorCount    int             `json:"error_count"`
	AllTime       *AllTimeTotals  `json:"all_time"`
}
```

- [ ] **Step 2: 修改 GetAccountStats 方法 — 末尾添加全时间统计查询**
文件: `pkg/store/store.go:747`（在 `return as, nil` 之前插入）

```go
	// All-time totals
	allTime := &AllTimeTotals{}
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ?", apiKey).Scan(&allTime.TotalRequests)
	s.db.QueryRow("SELECT COALESCE(SUM(input_tokens), 0) FROM request_logs WHERE api_key = ?", apiKey).Scan(&allTime.TotalInputTk)
	s.db.QueryRow("SELECT COALESCE(SUM(output_tokens), 0) FROM request_logs WHERE api_key = ?", apiKey).Scan(&allTime.TotalOutputTk)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ? AND status_code >= 400", apiKey).Scan(&allTime.ErrorCount)
	as.AllTime = allTime

	return as, nil
```

- [ ] **Step 3: 验证后端编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./...`
Expected:
  - Exit code: 0
  - No output

- [ ] **Step 4: 提交**
Run: `git add pkg/store/store.go && git commit -m "feat(store): add all-time totals to account stats API"`

---

### Task 2: Frontend — Display 24h and all-time stats in AccountDetail

**Depends on:** Task 1
**Files:**
- Modify: `web/src/api.ts:45-55`（AccountStats 接口）
- Modify: `web/src/pages/AccountDetail.tsx:404-467`（Stats 展示行）

- [ ] **Step 1: 更新 AccountStats 接口 — 添加 all_time 字段**
文件: `web/src/api.ts:45-55`

```typescript
export interface AccountStats {
  api_key: string;
  total_requests: number;
  total_input_tokens: number;
  total_output_tokens: number;
  by_model: { model: string; count: number }[];
  by_endpoint: { endpoint: string; count: number }[];
  avg_latency_ms: number;
  stream_count: number;
  error_count: number;
  all_time?: {
    total_requests: number;
    total_input_tokens: number;
    total_output_tokens: number;
    error_count: number;
  };
}
```

- [ ] **Step 2: 修改 AccountDetail Stats 行 — 每个卡片下方显示全部时间数据**
文件: `web/src/pages/AccountDetail.tsx:403-467`（替换整个 stats Row 区块）

```tsx
      {/* Stats row */}
      {stats && (
        <Row gutter={[12, 12]} style={{ marginBottom: 20 }}>
          <Col xs={8} sm={4}>
            <Card size="small" bodyStyle={{ padding: '12px 16px' }}>
              <Statistic
                title={<span style={{ fontSize: 12 }}>请求 <span style={{ color: '#999', fontWeight: 400 }}>(24h)</span></span>}
                value={stats.total_requests}
                prefix={<ApiOutlined />}
                valueStyle={{ fontSize: 20 }}
              />
              {stats.all_time && (
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>累计 {stats.all_time.total_requests.toLocaleString()}</Typography.Text>
              )}
            </Card>
          </Col>
          <Col xs={8} sm={4}>
            <Card size="small" bodyStyle={{ padding: '12px 16px' }}>
              <Statistic
                title={<span style={{ fontSize: 12 }}>输入 Token <span style={{ color: '#999', fontWeight: 400 }}>(24h)</span></span>}
                value={fmtTokens(stats.total_input_tokens || 0)}
                valueStyle={{ fontSize: 20 }}
              />
              {stats.all_time && (
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>累计 {fmtTokens(stats.all_time.total_input_tokens)}</Typography.Text>
              )}
            </Card>
          </Col>
          <Col xs={8} sm={4}>
            <Card size="small" bodyStyle={{ padding: '12px 16px' }}>
              <Statistic
                title={<span style={{ fontSize: 12 }}>输出 Token <span style={{ color: '#999', fontWeight: 400 }}>(24h)</span></span>}
                value={fmtTokens(stats.total_output_tokens || 0)}
                valueStyle={{ fontSize: 20 }}
              />
              {stats.all_time && (
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>累计 {fmtTokens(stats.all_time.total_output_tokens)}</Typography.Text>
              )}
            </Card>
          </Col>
          <Col xs={8} sm={4}>
            <Card size="small" bodyStyle={{ padding: '12px 16px' }}>
              <Statistic
                title={<span style={{ fontSize: 12 }}>平均延迟 <span style={{ color: '#999', fontWeight: 400 }}>(24h)</span></span>}
                value={Math.round(stats.avg_latency_ms)}
                suffix="ms"
                prefix={<ThunderboltOutlined />}
                valueStyle={{ fontSize: 20 }}
              />
            </Card>
          </Col>
          <Col xs={8} sm={4}>
            <Card size="small" bodyStyle={{ padding: '12px 16px' }}>
              <Statistic
                title={<span style={{ fontSize: 12 }}>成功率 <span style={{ color: '#999', fontWeight: 400 }}>(24h)</span></span>}
                value={successRate}
                suffix="%"
                prefix={<CheckCircleOutlined />}
                valueStyle={{ fontSize: 20, color: successRate >= 95 ? '#52c41a' : successRate >= 80 ? '#faad14' : '#ff4d4f' }}
              />
            </Card>
          </Col>
          <Col xs={8} sm={4}>
            <Card size="small" bodyStyle={{ padding: '12px 16px' }}>
              <Statistic
                title={<span style={{ fontSize: 12 }}>错误 <span style={{ color: '#999', fontWeight: 400 }}>(24h)</span></span>}
                value={stats.error_count}
                prefix={<WarningOutlined />}
                valueStyle={{ fontSize: 20, color: stats.error_count > 0 ? '#ff4d4f' : undefined }}
              />
              {stats.all_time && stats.all_time.error_count > 0 && (
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>累计 {stats.all_time.error_count}</Typography.Text>
              )}
            </Card>
          </Col>
        </Row>
      )}
```

- [ ] **Step 3: 构建前端并验证**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 4: 构建后端并重启服务**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o cmd/JoyCodeProxy/JoyCodeProxy ./cmd/JoyCodeProxy && lsof -ti:34891 | xargs kill 2>/dev/null; sleep 1; nohup cmd/JoyCodeProxy/JoyCodeProxy > /dev/null 2>> ~/.joycode-proxy/logs/stderr.log &`
Expected:
  - Exit code: 0
  - Service responds to health check

- [ ] **Step 5: 提交**
Run: `git add web/src/api.ts web/src/pages/AccountDetail.tsx cmd/JoyCodeProxy/static/ && git commit -m "feat(ui): show 24h and all-time stats dimensions on account detail page"`
