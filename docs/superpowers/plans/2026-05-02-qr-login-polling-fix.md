# Fix QR Login Polling — 扫码确认后前端无响应

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 修复京东 APP 扫码确认后前端界面无变化的 bug，确保 QR 登录流程完整可用。

**Architecture:** 前端 QRLoginModal 每 3s 轮询 `/api/qr-login/status` → 后端查询 JD `qr.m.jd.com/check` → JD 返回 JSONP 含 code(200=已确认,201=等待,202=已扫码) → 后端 code 200 时验证 ticket 获取 ptKey → 调用 AddAccount 存储 → 返回 `{status:"confirmed"}` 给前端。当前 bug：前端轮询链可能因 useEffect 依赖重渲染被中断，后端 AddAccount 失败时阻断 confirmed 状态返回。

**Tech Stack:** Go 1.24, React 19, TypeScript 5, Ant Design 5

**Risks:**
- Task 1 修改后端错误处理时需确保不改变 JD 协议交互逻辑 → 缓解：仅修改响应和日志
- Task 2 修改前端 useEffect 依赖时需确保 modal 关闭时正确 cleanup → 缓解：保持 cleanup 逻辑不变，用 ref 管理轮询状态

---

### Task 1: 修复后端 QR 登录轮询和错误处理

**Depends on:** None
**Files:**
- Modify: `pkg/dashboard/handler.go:303-364`（handleQRLoginStatus 函数）
- Modify: `pkg/auth/jdlogin.go:92-152`（QRPollStatus 函数，加日志）

- [ ] **Step 1: 修改 handleQRLoginStatus — AddAccount 失败时仍返回 confirmed 状态，添加详细日志**
文件: `pkg/dashboard/handler.go:329-364`（替换从 `if status != "confirmed"` 到函数结束）

```go
	if status != "confirmed" {
		slog.Debug("qr-login poll", "session", sessionID, "status", status)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": status,
		})
		return
	}

	apiKey := result.RealName
	if apiKey == "" {
		apiKey = result.UserID
	}

	isDefault := true
	accounts, _ := h.store.ListAccounts()
	for _, a := range accounts {
		if a.IsDefault {
			isDefault = false
			break
		}
	}

	if err := h.store.AddAccount(apiKey, result.PtKey, result.UserID, isDefault, "GLM-5.1"); err != nil {
		slog.Error("qr-login save account failed", "api_key", apiKey, "error", err)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "confirmed",
			"ok":      false,
			"api_key": apiKey,
			"user_id": result.UserID,
			"message": "登录成功但保存账号失败: " + err.Error(),
		})
		return
	}

	slog.Info("qr-login: account saved", "api_key", apiKey, "user_id", result.UserID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "confirmed",
		"ok":        true,
		"api_key":   apiKey,
		"user_id":   result.UserID,
		"real_name": result.RealName,
	})
```

- [ ] **Step 2: 修改 QRPollStatus — 添加关键日志帮助调试**
文件: `pkg/auth/jdlogin.go:104-152`（替换 check 请求到 switch 结束）

```go
	reqURL := fmt.Sprintf(qrCheckURL, url.QueryEscape(session.Token), time.Now().UnixMilli())
	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("User-Agent", jdUserAgent)
	resp, err := session.client.Do(req)
	if err != nil {
		slog.Error("qr-check request failed", "session", sessionID, "error", err)
		return "error", nil, err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	str := string(body)
	start := strings.Index(str, "(")
	end := strings.LastIndex(str, ")")
	if start < 0 || end < 0 {
		slog.Warn("qr-check response not JSONP", "session", sessionID, "body", str[:min(len(str), 200)])
		return "waiting", nil, nil
	}

	var check struct {
		Code   int    `json:"code"`
		Ticket string `json:"ticket,omitempty"`
	}
	if err := json.Unmarshal([]byte(str[start+1:end]), &check); err != nil {
		slog.Warn("qr-check JSONP parse failed", "session", sessionID, "payload", str[start+1:end])
		return "waiting", nil, nil
	}

	slog.Debug("qr-check result", "session", sessionID, "code", check.Code, "has_ticket", check.Ticket != "")

	switch check.Code {
	case 200:
		if check.Ticket == "" {
			slog.Error("qr-check code 200 but empty ticket", "session", sessionID)
			return "error", nil, fmt.Errorf("ticket is empty")
		}
		loginResult, err := validateAndFetchInfo(session.client, check.Ticket)
		if err != nil {
			slog.Error("qr-validate failed", "session", sessionID, "error", err)
			return "error", nil, err
		}
		slog.Info("qr-login confirmed", "session", sessionID, "user_id", loginResult.UserID, "real_name", loginResult.RealName)
		QRCleanup(sessionID)
		return "confirmed", loginResult, nil
	case 201:
		return "waiting", nil, nil
	case 202:
		return "scanned", nil, nil
	case 203, 204:
		slog.Info("qr-code expired", "session", sessionID, "code", check.Code)
		QRCleanup(sessionID)
		return "expired", nil, nil
	default:
		slog.Warn("qr-check unknown code", "session", sessionID, "code", check.Code)
		return "waiting", nil, nil
	}
```

同时在文件顶部添加 `min` 辅助函数（如果 Go 版本 < 1.21 没有内置 min）：

```go
// 放在 import 块之后，常量定义之前
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

然后将 Step 2 中的 `min(len(str), 200)` 替换为 `minInt(len(str), 200)`。

- [ ] **Step 3: 验证 Go 编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy`
Expected:
  - Exit code: 0
  - Output is empty (no errors)

- [ ] **Step 4: 提交**
Run: `git add pkg/dashboard/handler.go pkg/auth/jdlogin.go && git commit -m "fix(qr-login): add logging and preserve confirmed status on save error"`

---

### Task 2: 修复前端轮询链中断 + 构建部署

**Depends on:** Task 1
**Files:**
- Modify: `web/src/components/QRLoginModal.tsx:46-79`（polling useEffect）
- Modify: `web/src/components/QRLoginModal.tsx:1`（import useCallback 的使用方式）

- [ ] **Step 1: 修改 QRLoginModal — 用 ref 管理轮询避免 useEffect 依赖中断轮询链**

核心问题：当前 useEffect 依赖 `[open, sessionId, status, onSuccess, onClose]`，其中 `onSuccess` 和 `onClose` 每次 Accounts 重渲染时都是新引用，导致 useEffect cleanup 中断正在进行的轮询链。

修复方案：用 `useRef` 存储 `onSuccess`/`onClose`/`sessionId`，从 useEffect 依赖中移除它们，仅在 `open` 变化时启动/停止轮询。

文件: `web/src/components/QRLoginModal.tsx`（替换整个组件）

```tsx
import React, { useEffect, useState, useRef, useCallback } from 'react';
import { Modal, Typography, Button, Space, Alert, Spin } from 'antd';
import { ReloadOutlined, CheckCircleOutlined, CloseCircleOutlined } from '@ant-design/icons';
import { api } from '../api';

interface QRLoginModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

const QRLoginModal: React.FC<QRLoginModalProps> = ({ open, onClose, onSuccess }) => {
  const [qrImage, setQrImage] = useState('');
  const [status, setStatus] = useState<'loading' | 'waiting' | 'scanned' | 'confirmed' | 'expired' | 'error'>('loading');
  const [countdown, setCountdown] = useState(180);
  const [errorMsg, setErrorMsg] = useState('');
  const sessionIdRef = useRef('');
  const pollTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const onSuccessRef = useRef(onSuccess);
  const onCloseRef = useRef(onClose);

  onSuccessRef.current = onSuccess;
  onCloseRef.current = onClose;

  const initQR = useCallback(async () => {
    setStatus('loading');
    setCountdown(180);
    setErrorMsg('');
    try {
      const result = await api.qrLoginInit();
      setQrImage(result.qr_image);
      sessionIdRef.current = result.session_id;
      setStatus('waiting');
    } catch (e: unknown) {
      setStatus('error');
      setErrorMsg(e instanceof Error ? e.message : '生成二维码失败');
    }
  }, []);

  useEffect(() => {
    if (open) {
      initQR();
    } else {
      setQrImage('');
      sessionIdRef.current = '';
      setStatus('loading');
      if (pollTimerRef.current) clearTimeout(pollTimerRef.current);
    }
  }, [open, initQR]);

  useEffect(() => {
    if (!open) return;

    const poll = async () => {
      const sid = sessionIdRef.current;
      if (!sid) {
        pollTimerRef.current = setTimeout(poll, 1000);
        return;
      }
      try {
        const result = await api.qrLoginStatus(sid);
        if (result.status === 'confirmed') {
          setStatus('confirmed');
          setTimeout(() => {
            onSuccessRef.current();
            onCloseRef.current();
          }, 1500);
          return;
        }
        if (result.status === 'expired') {
          setStatus('expired');
          return;
        }
        if (result.status === 'error') {
          setStatus('error');
          setErrorMsg(result.message || '登录失败');
          return;
        }
        if (result.status === 'scanned') {
          setStatus('scanned');
        }
      } catch {
        // Continue polling on network error
      }
      pollTimerRef.current = setTimeout(poll, 3000);
    };

    pollTimerRef.current = setTimeout(poll, 2000);
    return () => { if (pollTimerRef.current) clearTimeout(pollTimerRef.current); };
  }, [open]);

  useEffect(() => {
    if (!open || status === 'confirmed' || status === 'expired' || status === 'loading') return;
    const timer = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          setStatus('expired');
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(timer);
  }, [open, status]);

  const statusDisplay = () => {
    switch (status) {
      case 'loading':
        return <div style={{ textAlign: 'center', padding: 40 }}><Spin size="large" /><div style={{ marginTop: 12, color: '#666' }}>正在生成二维码...</div></div>;
      case 'waiting':
        return <Alert type="info" message="请使用京东 APP 扫描上方二维码" description={`二维码有效期剩余 ${Math.floor(countdown / 60)}:${String(countdown % 60).padStart(2, '0')}`} showIcon />;
      case 'scanned':
        return <Alert type="success" message="已扫描，请在手机上确认登录..." showIcon />;
      case 'confirmed':
        return <Alert type="success" message="登录成功！账号已添加" showIcon icon={<CheckCircleOutlined />} />;
      case 'expired':
        return <Space direction="vertical" align="center" style={{ width: '100%' }}>
          <Alert type="warning" message="二维码已过期" showIcon icon={<CloseCircleOutlined />} />
          <Button icon={<ReloadOutlined />} onClick={initQR}>刷新二维码</Button>
        </Space>;
      case 'error':
        return <Space direction="vertical" align="center" style={{ width: '100%' }}>
          <Alert type="error" message={errorMsg || "登录失败"} showIcon />
          <Button icon={<ReloadOutlined />} onClick={initQR}>重试</Button>
        </Space>;
    }
  };

  return (
    <Modal
      title="扫码登录"
      open={open}
      onCancel={onClose}
      footer={null}
      width={400}
      centered
    >
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 16 }}>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          使用京东 APP 扫描二维码登录，每个京东账号对应一个 JoyCode 账号
        </Typography.Text>
        {qrImage && status !== 'confirmed' && (
          <div style={{
            padding: 12, background: '#fff', borderRadius: 8,
            border: '1px solid #f0f0f0', boxShadow: '0 2px 8px rgba(0,0,0,0.06)',
          }}>
            <img src={qrImage} alt="QR Code" style={{ width: 200, height: 200 }} />
          </div>
        )}
        {statusDisplay()}
      </div>
    </Modal>
  );
};

export default QRLoginModal;
```

关键改动：
1. `sessionId` 改为 `sessionIdRef`（ref），不再触发 useEffect 重渲染
2. `onSuccess`/`onClose` 改为 `useRef` 存储，避免每次 Accounts 重渲染导致 useEffect cleanup
3. 轮询 useEffect 仅依赖 `[open]`，轮询链不会被 status 变化中断
4. `setStatus('scanned')` 不再导致轮询链重建

- [ ] **Step 2: 验证 TypeScript 编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npx tsc --noEmit`
Expected:
  - Exit code: 0
  - Output does NOT contain: "error TS"

- [ ] **Step 3: 构建前端**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 4: 构建后端并重启**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy && ps aux | grep joycode_proxy_bin | grep -v grep | awk '{print $2}' | xargs kill`
Expected:
  - Go build exit code: 0
  - launchd 自动重启服务

- [ ] **Step 5: 验证服务**
Run: `sleep 3 && curl -s http://127.0.0.1:34891/api/health`
Expected: `{"status":"ok",...}`

- [ ] **Step 6: 提交**
Run: `git add web/src/components/QRLoginModal.tsx && git commit -m "fix(qr-login): stabilize polling chain with refs to prevent useEffect cleanup interrupting poll cycle"`

---

### Task 3: 构建部署

**Depends on:** Task 1, Task 2
**Files:** None

- [ ] **Step 1: 构建前端**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
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
Run: `sleep 3 && curl -s http://127.0.0.1:34891/api/health`
Expected: `{"status":"ok",...}`
