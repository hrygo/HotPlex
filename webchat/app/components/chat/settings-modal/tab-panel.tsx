import type { ReactNode } from 'react';

interface TabPanelProps {
  children: ReactNode;
  className?: string;
}

// 统一 settings tab 内容外壳。所有 tab（含将来新建）都应用 TabPanel 包裹，
// 使纵向间距与 AI Configuration tab（AgentConfigEditor 的 space-y-4 标杆）
// 一致，配合 settings/page.tsx 的 Section Content Card min-height 消除
// tab 切换时的高度抖动。
export function TabPanel({ children, className }: TabPanelProps) {
  return <div className={`space-y-4 ${className ?? ''}`}>{children}</div>;
}
