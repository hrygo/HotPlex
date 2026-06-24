// Workspace work_dir 沙箱路径构造。与后端 security.ValidateWorkspaceWorkDir 对齐：
// work_dir 必须落在 $HOME/.hotplex/workspaces/<ownerUserID>/<seg> 下。~/ 由后端
// config.ExpandAndAbs 展开为 $HOME，前后端约定一致。

// 将任意字符串规整为安全的单段目录名：小写 → 仅保留 [a-z0-9-] → 折叠连续 '-' → 去首尾 '-'。
// 空串回退为 "workspace"，避免空段。
export function sanitizeWorkspaceDir(name: string): string {
  const seg = name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/-{2,}/g, '-')
    .replace(/^-+|-+$/g, '');
  return seg || 'workspace';
}

// 构造 owner 沙箱内的 work_dir。subdir 留空时用 name 规整后的段。
export function buildWorkspaceWorkDir(uid: string, name: string, subdir?: string): string {
  const seg = subdir && subdir.trim() ? sanitizeWorkspaceDir(subdir) : sanitizeWorkspaceDir(name);
  return `~/.hotplex/workspaces/${uid}/${seg}`;
}
