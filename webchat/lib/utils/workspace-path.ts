// Workspace work_dir 沙箱路径构造。与后端 security.WorkspaceSandboxRoot 对齐：
// work_dir 必须落在服务端 workspace_root（HotplexHome()/workspaces/<segment>）下。
// root 由 List 响应 workspace_root 提供（绝对路径，服务端展开），前端不自行推导。

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

// subdir 多级支持（G2）：按 / 分段 sanitize，每段沿用 [a-z0-9-] 规则；
// 空段与 "." / ".."（路径逃逸）段直接丢弃，整体为空时回退 "workspace"。
export function sanitizeWorkspaceDirMulti(rel: string): string {
  return (
    rel
      .split('/')
      .map((seg) => seg.trim())
      .filter((seg) => seg !== '' && seg !== '.' && seg !== '..')
      .map(sanitizeWorkspaceDir)
      .filter(Boolean)
      .join('/') || 'workspace'
  );
}

// workspaceSandboxPrefix 归一化服务端 workspace_root（绝对路径，已含段名）为带尾随
// 分隔符的前缀。root 未知（旧后端）时调用方应降级禁用，不发送错误路径。
export function workspaceSandboxPrefix(root: string): string {
  return root.endsWith('/') ? root : root + '/';
}

// 构造服务端沙箱根下的 work_dir（绝对路径，用于创建表单）。subdir 留空时用 name
// 规整后的段；subdir 支持多级相对段（如 projects/myapp）。
export function buildWorkspaceWorkDir(root: string, name: string, subdir?: string): string {
  const seg = subdir && subdir.trim() ? sanitizeWorkspaceDirMulti(subdir) : sanitizeWorkspaceDir(name);
  return workspaceSandboxPrefix(root) + seg;
}

// 从实际 work_dir 自提取沙箱锚点（v3：无身份参数，兼容 UUID grandfather 根与
// username 根）。取最后一段 workspaces/，避免用户名/路径前缀误匹配。
//   - 命中 .../workspaces/<segment>/<seg> → { prefix: '.../workspaces/<segment>/', seg }
//   - 命中沙箱根本身 .../workspaces/<segment> → { prefix: work_dir, seg: '' }
//   - 不含 workspaces/ 结构（历史/异常） → null，调用方应禁用编辑并告警
export function resolveSandboxAnchor(workDir: string): { prefix: string; seg: string } | null {
  if (!workDir) return null;
  const anchor = 'workspaces/';
  const idx = workDir.lastIndexOf(anchor);
  if (idx < 0) return null;
  const after = workDir.slice(idx + anchor.length);
  const segEnd = after.indexOf('/');
  if (segEnd < 0) return { prefix: workDir, seg: '' };
  const seg = after.slice(0, segEnd);
  return { prefix: workDir.slice(0, idx + anchor.length + seg.length + 1), seg: after.slice(segEnd + 1) };
}

// 将沙箱锚点 prefix 与可编辑段重拼为完整 work_dir（Settings 保存路径）：
// 段为空（沙箱根本身）→ 返回去尾斜杠的 prefix；段非空 → 保证 prefix 与段间恰有一个
// 分隔符，且多级段（projects/myapp）原样保留（评审 F3/F4 修复）。
export function rejoinWorkDir(prefix: string, seg: string): string {
  const clean = seg.trim() ? sanitizeWorkspaceDirMulti(seg) : '';
  const base = prefix.replace(/\/+$/, '');
  if (!clean) return base;
  return `${base}/${clean}`;
}
