# Sidebar Green Theme & Remove Broken QR Login

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 侧边栏配色改为绿色 (#00b578) 扁平风格与其他页面一致；移除无法工作的 QR 扫码登录功能。

**Architecture:** MainLayout Sider 组件改为绿色背景 + 白色文字菜单；前端移除 QRLoginModal 组件引用和相关 API 调用；后端保留 QR 登录 API 不动（向后兼容）。

**Tech Stack:** React 19, TypeScript 5, Ant Design 5

**Risks:**
- Task 1 修改 Sider 背景色可能影响菜单选中态可见性 → 缓解：使用 Ant Design Menu 的 dark theme 模式
- Task 2 移除 QR 登录按钮不影响已有账号管理功能 → 手动添加账号入口保留

---

### Task 1: 修改侧边栏为绿色主题 — 与其他页面配色保持一致

**Depends on:** None
**Files:**
- Modify: `web/src/layouts/MainLayout.tsx:44-67`（Sider 和 Menu 组件）

- [ ] **Step 1: 修改 MainLayout Sider 为绿色主题**
文件: `web/src/layouts/MainLayout.tsx`（替换整个 Sider 和内部内容区域）

```tsx
const MainLayout: React.FC = () => {
  const [collapsed, setCollapsed] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();
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
        theme="dark"
        style={{
          background: 'linear-gradient(180deg, #009a63 0%, #007a4d 100%)',
        }}
      >
        <div style={{
          height: 48,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          borderBottom: '1px solid rgba(255,255,255,0.15)',
        }}>
          <img src="/favicon.ico" alt="JoyCode" style={{ width: 24, height: 24, marginRight: collapsed ? 0 : 8 }} />
          {!collapsed && <Text strong style={{ fontSize: 15, color: '#fff' }}>JoyCode 代理</Text>}
        </div>
        <Menu
          mode="inline"
          theme="dark"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
          style={{ background: 'transparent', borderRight: 'none' }}
        />
      </Sider>
      <Layout>
        <Header style={{
          padding: '0 24px',
          background: '#fff',
          borderBottom: '1px solid #f0f0f0',
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
```

同时移除未使用的 `theme` import（`theme.useToken` 不再需要）。更新 import：

```tsx
import { Layout, Menu, Typography, Tag } from 'antd';
```

移除 `const { token } = theme.useToken();` 行。

- [ ] **Step 2: 验证 TypeScript 编译**
Run: `cd web && npx tsc --noEmit`
Expected:
  - Exit code: 0
  - Output does NOT contain: "error TS"

- [ ] **Step 3: 提交**
Run: `git add web/src/layouts/MainLayout.tsx && git commit -m "refactor(layout): apply green gradient theme to sidebar for visual consistency"`

---

### Task 2: 移除 QR 扫码登录功能 — 移除前端无效的扫码入口

**Depends on:** None
**Files:**
- Modify: `web/src/pages/Accounts.tsx`（移除 QR 登录按钮和 QRLoginModal 引用）

- [ ] **Step 1: 读取 Accounts.tsx 确认 QR 登录按钮位置**
（读取文件以确认修改范围）

- [ ] **Step 2: 从 Accounts.tsx 移除 QRLoginModal 引用和扫码登录按钮**
移除以下内容：
1. `import QRLoginModal from '../components/QRLoginModal'` 导入
2. QR 登录相关的 state（`qrModalOpen`, `setQrModalOpen`）
3. 「扫码登录」按钮
4. `<QRLoginModal>` 组件渲染

- [ ] **Step 3: 验证 TypeScript 编译**
Run: `cd web && npx tsc --noEmit`
Expected:
  - Exit code: 0

- [ ] **Step 4: 提交**
Run: `git add web/src/pages/Accounts.tsx && git commit -m "refactor(accounts): remove non-functional QR login button (JD app incompatible)"`

---

### Task 3: 构建部署

**Depends on:** Task 1, Task 2
**Files:** None

- [ ] **Step 1: 构建前端**
Run: `cd web && npm run build`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 2: 构建后端二进制**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy`
Expected:
  - Exit code: 0

- [ ] **Step 3: 重启服务**
Run: `ps aux | grep joycode_proxy_bin | grep -v grep | awk '{print $2}' | xargs kill`
Expected: launchd 自动重启

- [ ] **Step 4: 验证**
Run: `sleep 2 && curl -s http://localhost:34891/api/health`
Expected: `{"status":"ok",...}`
