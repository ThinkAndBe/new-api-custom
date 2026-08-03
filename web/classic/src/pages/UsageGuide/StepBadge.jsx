import React from 'react';
import { Check } from 'lucide-react';

const StepBadge = ({ step, ready, readyBackground, idleBackground, size = 28, content }) => {
  const background = ready
    ? readyBackground || 'var(--semi-color-success)'
    : idleBackground || 'var(--semi-color-primary)';
  const fontSize = Math.max(12, Math.round(size * 0.5));
  return (
    <div
      style={{
        width: size,
        height: size,
        borderRadius: '50%',
        background,
        color: '#fff',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontSize,
        fontWeight: 'bold',
        flexShrink: 0,
      }}
    >
      {ready ? <Check size={Math.round(size * 0.6)} /> : content || step}
    </div>
  );
};

export default StepBadge;
