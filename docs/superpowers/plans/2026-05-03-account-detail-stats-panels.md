# AccountDetail 统计面板增强 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 将 AccountDetail 页面的 6 个扁平统计卡片重构成两个结构化面板（请求统计面板 + Token 消费面板），每个面板包含更丰富的维度指标，风格对齐 Dashboard 页面。

**Architecture:** 后端 AccountStats 补充 success_count 字段 → 前端 AccountStats 接口同步 → AccountDetail 页面将 stats 行从 6 个独立 Statistic 卡片替换为两个 Card 面板（请求统计 + Token 消费），面板内部用 Row/Col 布局展示多维度指标。

**Tech Stack:** Go 1.22, React 19, Ant Design 5, TypeScript 5, Recharts 2

**Risks:**
- 后端补充字段是纯增量操作，无兼容性风险
- 前端重构替换整段 JSX，需确保不遗漏已有功能（24h + 累计标签、颜色标识等）

---

### Task 1: 后端补充 success_count 字段

**Depends on:** None
**Files:**
- Modify: `pkg/store/store.go:69-81`（AccountStats struct）
- Modify: `pkg/store/store.go:717-722`（GetAccountStats 查询）

- [ ] **Step 1: 修改 AccountStats struct 添加 SuccessCount 字段**
文件: `pkg/store/store.go:69-81`

```go
// 替换 AccountStats struct（store.go:69-81）
type AccountStats struct {
	APIKey        string          `json:"api_key"`
	TotalRequests int             `json:"total_requests"`
	TotalInputTk  int             `json:"total_input_tokens"`
	TotalOutputTk int             `json:"total_output_tokens"`
	SuccessCount  int             `json:"success_count"`
	StreamCount   int             `json:"stream_count"`
	ByModel       []ModelCount    `json:"by_model"`
	ByEndpoint    []EndpointCount `json:"by_endpoint"`
	AvgLatencyMs  float64         `json:"avg_latency_ms"`
	ErrorCount    int             `json:"error_count"`
	AllTime       *AllTimeTotals  `json:"all_time"`
	Hourly        []HourlyData    `json:"hourly"`
}
```

- [ ] **Step 2: 修改 GetAccountStats 函数添加 success_count 查询**
文件: `pkg/store/store.go:717-722`（在 ErrorCount 查询行之后添加 SuccessCount 查询）

在 `GetAccountStats` 函数中，找到查询 `ErrorCount` 的行（约 store.go:720）：
```go
s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ? AND status_code >= 400 AND "+tf, apiKey).Scan(&as.ErrorCount)
```
在此行之后添加：
```go
s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ? AND status_code < 400 AND "+tf, apiKey).Scan(&as.SuccessCount)
```

- [ ] **Step 3: 验证后端编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./...`
Expected:
  - Exit code: 0
  - Output does NOT contain: "Error" or "cannot"

- [ ] **Step 4: 提交**
Run: `git add pkg/store/store.go && git commit -m "feat(store): add success_count to AccountStats for richer panel metrics"`

---

### Task 2: 前端重构统计行为双面板布局

**Depends on:** Task 1
**Files:**
- Modify: `web/src/api.ts:45-68`（AccountStats 接口）
- Modify: `web/src/pages/AccountDetail.tsx:404-479`（stats 行替换为面板）

- [ ] **Step 1: 更新 AccountStats TypeScript 接口添加 success_count**
文件: `web/src/api.ts:45-68`

在 `AccountStats` 接口中，`total_output_tokens` 之后、`by_model` 之前，添加：
```typescript
success_count: number;
stream_count: number;
```

注意：`stream_count` 已存在于接口中，只需添加 `success_count`。最终接口中 `total_output_tokens` 之后应有：
```typescript
  total_output_tokens: number;
  success_count: number;
  by_model: { model: string; count: number }[];
```

- [ ] **Step 2: 重构 AccountDetail.tsx 统计行为两个面板**

首先在 import 中添加 `Divider, CloseCircleOutlined, SwapOutlined`：
文件: `web/src/pages/AccountDetail.tsx:2-5`，在 antd import 的 `Tooltip,` 之后、右括号前添加 `Divider`。
在 icons import 中添加 `CloseCircleOutlined, SwapOutlined`。

然后替换 stats 行 JSX。找到 `web/src/pages/AccountDetail.tsx:404-479`（`{/* Stats row */}` 到 `</Row>` 闭合）。

替换为以下两个面板：

```tsx
      {/* Stats panels */}
      {stats && (
        <Row gutter={[16, 16]} style={{ marginBottom: 20 }}>
          {/* 请求统计面板 */}
          <Col xs={24} md={12}>
            <Card
              title={<span><ApiOutlined style={{ marginRight: 6 }} />请求统计</span>}
              size="small"
              style={{ borderRadius: 8, height: '100%' }}
            >
              <Row gutter={[8, 12]}>
                <Col span={12}>
                  <Statistic title="今日请求" value={stats.total_requests} valueStyle={{ fontSize: 20, color: '#00b578' }} prefix={<ApiOutlined />} />
                </Col>
                <Col span={12}>
                  <Statistic title="累计请求" value={stats.all_time?.total_requests ?? 0} valueStyle={{ fontSize: 20 }} />
                </Col>
                <Col span={12}>
                  <Statistic
                    title="今日成功"
                    value={stats.success_count}
                    prefix={<CheckCircleOutlined />}
                    valueStyle={{ fontSize: 18, color: '#52c41a' }}
                  />
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    占比 {stats.total_requests > 0 ? Math.round((stats.success_count / stats.total_requests) * 100) : 100}%
                  </Typography.Text>
                </Col>
                <Col span={12}>
                  <Statistic
                    title="今日失败"
                    value={stats.error_count}
                    prefix={<CloseCircleOutlined />}
                    valueStyle={{ fontSize: 18, color: stats.error_count > 0 ? '#ff4d4f' : '#52c41a' }}
                  />
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    占比 {stats.total_requests > 0 ? Math.round((stats.error_count / stats.total_requests) * 100) : 0}%
                  </Typography.Text>
                </Col>
                <Col span={24}>
                  <Divider style={{ margin: '4px 0 8px' }} />
                  <Row gutter={8}>
                    <Col span={12}>
                      <Statistic
                        title="流式请求"
                        value={stats.stream_count}
                        prefix={<SwapOutlined />}
                        valueStyle={{ fontSize: 16 }}
                      />
                      <Tag color="blue" style={{ marginTop: 2 }}>
                        {stats.total_requests > 0 ? Math.round((stats.stream_count / stats.total_requests) * 100) : 0}%
                      </Tag>
                    </Col>
                    <Col span={12}>
                      <Statistic
                        title="平均延迟"
                        value={Math.round(stats.avg_latency_ms)}
                        suffix="ms"
                        prefix={<ThunderboltOutlined />}
                        valueStyle={{ fontSize: 16, color: stats.avg_latency_ms < 500 ? '#52c41a' : stats.avg_latency_ms < 1500 ? '#faad14' : '#ff4d4f' }}
                      />
                    </Col>
                  </Row>
                </Col>
              </Row>
            </Card>
          </Col>

          {/* Token 消费面板 */}
          <Col xs={24} md={12}>
            <Card
              title={<span><FireOutlined style={{ marginRight: 6 }} />Token 消费</span>}
              size="small"
              style={{ borderRadius: 8, height: '100%' }}
            >
              <Row gutter={[8, 12]}>
                <Col span={12}>
                  <Statistic
                    title="今日 Token"
                    value={fmtTokens(stats.total_input_tokens + stats.total_output_tokens)}
                    valueStyle={{ fontSize: 20, color: '#389e0d' }}
                  />
                </Col>
                <Col span={12}>
                  <Statistic
                    title="累计 Token"
                    value={fmtTokens((stats.all_time?.total_input_tokens ?? 0) + (stats.all_time?.total_output_tokens ?? 0))}
                    valueStyle={{ fontSize: 20 }}
                  />
                </Col>
                <Col span={12}>
                  <Statistic title="今日输入" value={fmtTokens(stats.total_input_tokens)} valueStyle={{ fontSize: 16 }} />
                </Col>
                <Col span={12}>
                  <Statistic title="今日输出" value={fmtTokens(stats.total_output_tokens)} valueStyle={{ fontSize: 16 }} />
                </Col>
                <Col span={24}>
                  <Divider style={{ margin: '4px 0 8px' }} />
                  <Row gutter={8}>
                    <Col span={12}>
                      <Statistic
                        title="平均每请求"
                        value={stats.total_requests > 0 ? fmtTokens(Math.round((stats.total_input_tokens + stats.total_output_tokens) / stats.total_requests)) : '-'}
                        suffix={stats.total_requests > 0 ? 'tokens' : ''}
                        valueStyle={{ fontSize: 15 }}
                      />
                    </Col>
                    <Col span={12}>
                      <Statistic
                        title="输入/输出比"
                        value={stats.total_output_tokens > 0 ? (stats.total_input_tokens / stats.total_output_tokens).toFixed(1) : '-'}
                        suffix={stats.total_output_tokens > 0 ? ':1' : ''}
                        valueStyle={{ fontSize: 15 }}
                      />
                    </Col>
                  </Row>
                </Col>
              </Row>
            </Card>
          </Col>
        </Row>
      )}
```

同时删除之前已不再使用的 `successRate` 变量定义（AccountDetail.tsx:177-179），因为面板内已内联计算。如果该变量在后续代码中无引用，直接移除。

- [ ] **Step 3: 构建前端**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 4: 构建 Go 二进制并重启服务**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/ && launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist && sleep 1 && curl -s http://localhost:34891/api/health`
Expected:
  - Exit code: 0
  - Output contains: `"status":"ok"`

- [ ] **Step 5: 提交**
Run: `git add web/src/api.ts web/src/pages/AccountDetail.tsx && git commit -m "feat(ui): refactor AccountDetail stats into dual-panel layout (requests + tokens)"`
