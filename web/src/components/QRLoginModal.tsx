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
  const [sessionId, setSessionId] = useState('');
  const [status, setStatus] = useState<'loading' | 'waiting' | 'scanned' | 'confirmed' | 'expired' | 'error'>('loading');
  const [countdown, setCountdown] = useState(180);
  const [errorMsg, setErrorMsg] = useState('');
  const pollRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  const initQR = useCallback(async () => {
    setStatus('loading');
    setCountdown(180);
    setErrorMsg('');
    try {
      const result = await api.qrLoginInit();
      setQrImage(result.qr_image);
      setSessionId(result.session_id);
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
      setSessionId('');
      setStatus('loading');
      if (pollRef.current) clearTimeout(pollRef.current);
    }
  }, [open, initQR]);

  useEffect(() => {
    if (!open || !sessionId || status === 'confirmed' || status === 'expired' || status === 'error' || status === 'loading') {
      return;
    }

    const poll = async () => {
      try {
        const result = await api.qrLoginStatus(sessionId);
        if (result.status === 'confirmed') {
          setStatus('confirmed');
          setTimeout(() => { onSuccess(); onClose(); }, 1500);
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
      pollRef.current = setTimeout(poll, 3000);
    };

    pollRef.current = setTimeout(poll, 2000);
    return () => { if (pollRef.current) clearTimeout(pollRef.current); };
  }, [open, sessionId, status, onSuccess, onClose]);

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
