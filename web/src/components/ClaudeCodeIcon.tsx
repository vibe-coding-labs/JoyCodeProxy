import React from 'react';

const SvgClaudeCode: React.FC<{ style?: React.CSSProperties }> = ({ style }) => (
  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" style={style}>
    <path d="M17.5 3H6.5C4.567 3 3 4.567 3 6.5v11C3 19.433 4.567 21 6.5 21h11c1.933 0 3.5-1.567 3.5-3.5V6.5C21 4.567 19.433 3 17.5 3z" fill="#D97757"/>
    <path d="M8.5 8.5l3.5 3.5-3.5 3.5M13 15.5h3" stroke="white" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"/>
  </svg>
);

export default SvgClaudeCode;
