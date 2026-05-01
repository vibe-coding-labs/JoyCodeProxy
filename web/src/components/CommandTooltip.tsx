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
