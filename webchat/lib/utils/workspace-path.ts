// Workspace work_dir 沙箱路径构造。与后端 security.ValidateWorkspaceWorkDir 对齐：
// work_dir 必须落在 $HOME/.hotplex/workspaces/<ownerUserID>/<seg> 下。
//
// 注意路径形态差异：前端创建表单用 ~/ 形式，后端 ExpandAndAbs 把它展开成
// $HOME 绝对路径存储（如 /Users/foo/.hotplex/workspaces/<uid>/seg）。因此反解
// 沙箱段时不能假定固定前缀形态，必须用 owner_id 作锚点从实际 work_dir 反推。

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

// 构造 owner 沙箱内的 work_dir（~/ 形式，用于创建表单）。subdir 留空时用 name 规整后的段。
export function buildWorkspaceWorkDir(uid: string, name: string, subdir?: string): string {
  const seg = subdir && subdir.trim() ? sanitizeWorkspaceDir(subdir) : sanitizeWorkspaceDir(name);
  return `${workspaceSandboxPrefix(uid)}${seg}`;
}

// owner 的 workspace 沙箱前缀 ~/ 形式（含尾随分隔符）。仅用于创建表单占位。
export function workspaceSandboxPrefix(ownerId: string): string {
  return `~/.hotplex/workspaces/${ownerId}/`;
}

// 从实际 work_dir 反推沙箱锚点，供 Settings 前缀只读 + 段编辑使用。owner_id 作锚点，
// 不依赖 work_dir 是 ~/ 还是绝对路径形态。
//   - 命中 .../workspaces/<ownerId>/<seg> → { prefix: '.../workspaces/<ownerId>/', seg }
//   - 命中沙箱根本身 .../workspaces/<ownerId> → { prefix, seg: '' }
//   - 不在 owner 沙箱下（历史/异常） → null，调用方应禁用编辑并告警
export function resolveSandboxAnchor(workDir: string, ownerId: string): { prefix: string; seg: string } | null {
  if (!workDir || !ownerId) return null;
  const anchor = `workspaces/${ownerId}/`;
  const idx = workDir.indexOf(anchor);
  if (idx >= 0) {
    return { prefix: workDir.slice(0, idx + anchor.length), seg: workDir.slice(idx + anchor.length) };
  }
  // 兼容沙箱根本身（无尾随分隔符）。
  const root = anchor.slice(0, -1);
  const ridx = workDir.lastIndexOf(root);
  if (ridx >= 0) {
    return { prefix: workDir.slice(0, ridx + root.length), seg: '' };
  }
  return null;
}
