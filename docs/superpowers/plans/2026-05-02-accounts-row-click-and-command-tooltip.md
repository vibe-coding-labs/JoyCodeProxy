# Accounts 行点击导航修复 + 命令预览 Tooltip 实现 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 修复账号列表页无法通过点击行空白处进入详情页的 bug；为 Claude Code / Codex 快速启动按钮添加带语法高亮的命令预览 Tooltip。

**Architecture:** 创建 `CommandTooltip` 组件封装 Ant Design `Tooltip`，接收命令字符串，内部用 `<pre>` 渲染带语法高亮的 shell 命令。Accounts 页面恢复 `onRow.onClick` 导航到详情页（所有操作按钮保留 `stopPropagation`）。快速启动列和 AccountDetail 页的 Tooltip 替换为 CommandTooltip。

**Tech Stack:** React 18, Ant Design 5 (Tooltip), TypeScript 5

**Risks:**
- 恢复 `onRow` 点击导航可能与操作列按钮事件冒泡冲突 → 缓解：所有操作列按钮已有 `e.stopPropagation()`，Popconfirm 内部按钮也需确认 stopPropagation 生效
- Tooltip 内长命令文本溢出 → 缓解：`overlayStyle` 设置 `maxWidth: 480px`，`<pre>` 设置 `whiteSpace: 'pre-wrap'` 自动换行

---

### Task 1: 创建 CommandTooltip 组件 — 带语法高亮的命令预览弹出框

**Depends on:** None
**Files:**
- Create: `web/src/components/CommandTooltip.tsx`

- [ ] **Step 1: 创建 CommandTooltip 组件 — 接收命令文本和子元素，渲染带语法高亮的 Tooltip**

```typescript
// web/src/components/CommandTooltip.tsx
import React from 'react';
import { Tooltip } from 'antd';

interface CommandTooltipProps {
  command: string;
  label: string;
  children: React.ReactElement;
}

const highlightShell = (cmd: string): React.ReactNode[] => {
  return cmd.split('\n').map((line, lineIdx, lines) => {
    const envMatch = line.match(/^([A-Z_]+)(=)(.*?)(\s*\\)?$/);
    const cmdMatch = line.match(/^(claude|codex)(\s+.*)?$/);
    let nodes: React.ReactNode[];

    if (envMatch) {
      const [, key, eq, value, trailing] = envMatch;
      nodes = [
        <span key="k" style={{ color: '#c678dd' }}>{key}</span>,
        <span key="e" style={{ color: '#56b6c2' }}>{eq}</span>,
        <span key="v" style={{ color: '#98c379' }}>{value}</span>,
      ];
      if (trailing) nodes.push(<span key="t" style={{ color: '#abb2bf' }}>{trailing}</span>);
    } else if (cmdMatch) {
      const [, cmd, rest] = cmdMatch;
      nodes = [
        <span key="c" style={{ color: '#61afef', fontWeight: 600 }}>{cmd}</span>,
        <span key="r" style={{ color: '#e5c07b' }}>{rest || ''}</span>,
      ];
    } else {
      nodes = [<span key="t" style={{ color: '#abb2bf' }}>{line}</span>];
    }

    const isLast = lineIdx === lines.length - 1;
    return (
      <React.Fragment key={lineIdx}>
        {nodes}
        {!isLast && '\n'}
      </React.Fragment>
    );
  });
};

const CommandTooltip: React.FC<CommandTooltipProps> = ({ command, label, children }) => (
  <Tooltip
    title={
      <div style={{ padding: '4px 0' }}>
        <div style={{ fontSize: 11, color: 'rgba(255,255,255,0.65)', marginBottom: 4 }}>
          {label} 命令（点击复制）
        </div>
        <pre style={{
          margin: 0, fontFamily: "'SF Mono', 'Fira Code', Consolas, monospace",
          fontSize: 11, lineHeight: 1.6, whiteSpace: 'pre-wrap', color: '#abb2bf',
          background: '#282c34', borderRadius: 4, padding: '6px 8px',
        }}>
          {highlightShell(command)}
        </pre>
      </div>
    }
    overlayStyle={{ maxWidth: 520 }}
    placement="left"
  >
    {children}
  </Tooltip>
);

export default CommandTooltip;
```

- [ ] **Step 2: 验证组件编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npx tsc --noEmit 2>&1 | head -20`
Expected:
  - Exit code: 0
  - No error output mentioning CommandTooltip

- [ ] **Step 3: 提交**
Run: `git add web/src/components/CommandTooltip.tsx && git commit -m "feat(web): add CommandTooltip component with shell syntax highlighting"`

---

### Task 2: 修复 Accounts 页面行点击导航 + 替换 Tooltip 为 CommandTooltip

**Depends on:** Task 1
**Files:**
- Modify: `web/src/pages/Accounts.tsx:1-14`（添加 import）
- Modify: `web/src/pages/Accounts.tsx:150-163`（账户名列：移除单独 onClick，改由 onRow 处理）
- Modify: `web/src/pages/Accounts.tsx:196-214`（快速启动列：Tooltip → CommandTooltip）
- Modify: `web/src/pages/Accounts.tsx:281-288`（Table：添加 onRow 点击导航）

- [ ] **Step 1: 添加 CommandTooltip import — 引入新组件**

文件: `web/src/pages/Accounts.tsx:1-14`（替换 import 区块）

```typescript
import React, { useEffect, useState } from 'react';
import {
  Table, Button, Space, Modal, Form, Input, Switch, Select,
  message, Popconfirm, Tag, Typography, Alert,
} from 'antd';
import {
  PlusOutlined, DeleteOutlined, StarOutlined,
  SafetyCertificateOutlined, ReloadOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons';
import SvgClaudeCode from '../components/ClaudeCodeIcon';
import SvgCodex from '../components/CodexIcon';
import CommandTooltip from '../components/CommandTooltip';
import { useNavigate } from 'react-router-dom';
import { api } from '../api';
import type { Account } from '../api';
```

- [ ] **Step 2: 修改账户名列 render — 移除单独 onClick，改由 onRow 统一处理导航**

文件: `web/src/pages/Accounts.tsx:150-163`（替换第一个 column 的 render）

原来的账户名列有独立的 `onClick` 导航。现在改回简单展示，导航逻辑由 `onRow` 统一处理。

```tsx
    {
      title: '账户名',
      dataIndex: 'api_key',
      key: 'api_key',
      render: (text: string) => (
        <Typography.Text strong>{text}</Typography.Text>
      ),
    },
```

- [ ] **Step 3: 替换快速启动列 Tooltip 为 CommandTooltip — 展示完整带高亮的命令**

文件: `web/src/pages/Accounts.tsx:196-214`（替换快速启动列 render）

```tsx
    {
      title: '快速启动',
      key: 'quickstart',
      width: 90,
      render: (_: unknown, record: Account) => {
        const claudeCmd = claudeCodeCmd(record.api_token, record.default_model || undefined);
        const cxCmd = codexCmd(record.api_token, record.default_model || undefined);
        return (
          <Space size={4}>
            <CommandTooltip command={claudeCmd} label="Claude Code">
              <Button
                type="text"
                size="small"
                icon={<SvgClaudeCode />}
                onClick={(e) => { e.stopPropagation(); copyToClipboard(claudeCmd, 'Claude Code'); }}
              />
            </CommandTooltip>
            <CommandTooltip command={cxCmd} label="Codex">
              <Button
                type="text"
                size="small"
                icon={<SvgCodex />}
                onClick={(e) => { e.stopPropagation(); copyToClipboard(cxCmd, 'Codex'); }}
              />
            </CommandTooltip>
          </Space>
        );
      },
    },
```

- [ ] **Step 4: 恢复 Table 的 onRow 点击导航 — 点击行空白处进入详情页**

文件: `web/src/pages/Accounts.tsx:281-288`（替换 Table 组件）

```tsx
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
        locale={{ emptyText: '暂无账号，请点击「一键登录」或「手动添加」按钮配置您的第一个 JoyCode 账号' }}
      />
```

- [ ] **Step 5: 验证前端构建**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build 2>&1 | tail -5`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 6: 提交**
Run: `git add web/src/pages/Accounts.tsx && git commit -m "fix(web): restore accounts row click navigation, add command preview tooltips"`

---

### Task 3: 更新 AccountDetail 页面 Tooltip 为 CommandTooltip

**Depends on:** Task 1
**Files:**
- Modify: `web/src/pages/AccountDetail.tsx:1-20`（添加 import、移除 Tooltip import）
- Modify: `web/src/pages/AccountDetail.tsx:307-312`（Claude Code 复制按钮 Tooltip → CommandTooltip）
- Modify: `web/src/pages/AccountDetail.tsx:328-333`（Codex 复制按钮 Tooltip → CommandTooltip）

- [ ] **Step 1: 更新 import — 添加 CommandTooltip，移除未使用的 Tooltip**

文件: `web/src/pages/AccountDetail.tsx:1-20`（替换 import 区块）

```typescript
import React, { useEffect, useState } from 'react';
import {
  Card, Row, Col, Statistic, Typography, Spin, Tag, Select, Button,
  message, Space, Table, Badge, Segmented, Tooltip, Popconfirm,
} from 'antd';
import {
  ArrowLeftOutlined, ApiOutlined, ThunderboltOutlined,
  CheckCircleOutlined, WarningOutlined, ReloadOutlined,
  ClockCircleOutlined, GlobalOutlined, FireOutlined, CopyOutlined,
  DeleteOutlined,
} from '@ant-design/icons';
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip as RTooltip,
  ResponsiveContainer, PieChart, Pie, Cell,
} from 'recharts';
import { useParams, useNavigate } from 'react-router-dom';
import { api } from '../api';
import type { Account, AccountStats, ModelInfo, RequestLog } from '../api';
import SvgClaudeCode from '../components/ClaudeCodeIcon';
import SvgCodex from '../components/CodexIcon';
import CommandTooltip from '../components/CommandTooltip';
```

- [ ] **Step 2: 替换 Claude Code 区域的复制按钮 Tooltip — 使用 CommandTooltip 展示完整命令**

文件: `web/src/pages/AccountDetail.tsx:307-312`（替换 Tooltip 包裹的复制按钮）

```tsx
                <CommandTooltip
                  command={buildClaudeCodeCmd(account.api_token, account.default_model || undefined)}
                  label="Claude Code"
                >
                  <Button
                    type="text" size="small" icon={<CopyOutlined />}
                    onClick={() => copyCmd(buildClaudeCodeCmd(account.api_token, account.default_model || undefined), 'Claude Code')}
                  />
                </CommandTooltip>
```

- [ ] **Step 3: 替换 Codex 区域的复制按钮 Tooltip — 使用 CommandTooltip 展示完整命令**

文件: `web/src/pages/AccountDetail.tsx:328-333`（替换 Tooltip 包裹的复制按钮）

```tsx
                <CommandTooltip
                  command={buildCodexCmd(account.api_token, account.default_model || undefined)}
                  label="Codex"
                >
                  <Button
                    type="text" size="small" icon={<CopyOutlined />}
                    onClick={() => copyCmd(buildCodexCmd(account.api_token, account.default_model || undefined), 'Codex')}
                  />
                </CommandTooltip>
```

- [ ] **Step 4: 验证前端构建**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build 2>&1 | tail -5`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 5: 提交**
Run: `git add web/src/pages/AccountDetail.tsx && git commit -m "feat(web): add command preview tooltip to AccountDetail page"`

---

### Task 4: 构建部署和验证

**Depends on:** Task 2, Task 3
**Files:**
- Modify: `cmd/JoyCodeProxy/static/`（前端产物）

- [ ] **Step 1: 构建 Go 二进制**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 2: 部署到本地服务**
Run: `launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist 2>/dev/null; sleep 1; launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist && sleep 2 && curl -s http://localhost:34891/api/health | python3 -m json.tool`
Expected:
  - Returns JSON with `status: "ok"`

- [ ] **Step 3: 验证账号列表页可访问**
Run: `curl -s -o /dev/null -w "%{http_code}" http://localhost:34891/accounts`
Expected:
  - HTTP 200

- [ ] **Step 4: 提交**
Run: `git add cmd/JoyCodeProxy/static/ && git commit -m "build: deploy with command tooltip and row click navigation fix"`
