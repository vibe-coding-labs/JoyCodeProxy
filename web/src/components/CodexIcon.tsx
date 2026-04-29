import React from 'react';

const SvgCodex: React.FC<{ style?: React.CSSProperties }> = ({ style }) => (
  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" style={style}>
    <rect x="3" y="3" width="18" height="18" rx="3.5" fill="#10A37F"/>
    <path d="M7 12l3 3 4-6 3 3" stroke="white" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"/>
  </svg>
);

export default SvgCodex;
