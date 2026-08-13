import { test, expect } from '@playwright/test';

// UI E2E for spec Root-HotplexHome: webchat 新建 workspace 表单必须使用服务端
// workspace_root（HOTPLEX_HOME 部署），预览不得含 ~/.hotplex。
// 手动 DoD 验收（§8.3）——需要真实 gateway 部署，CI 默认跳过：
//   export HOTPLEX_HOME=/tmp/hx-ws-e2e
//   hotplex gateway start -d                      # 127.0.0.1:18888，admin create 建 alice
//   HOTPLEX_E2E_WS_ROOT=1 pnpm exec playwright test e2e/workspace-root-hotplexhome.spec.ts
test.describe('workspace root follows HOTPLEX_HOME (UI)', () => {
  test.skip(!process.env.HOTPLEX_E2E_WS_ROOT, 'requires a HOTPLEX_HOME gateway deployment on :18888 (manual DoD)');

  test('create form previews server root and creates under it', async ({ page }) => {
    // 唯一 run id：work_dir 每用户唯一，重复运行不得 409 WORK_DIR_TAKEN。
    const runId = Date.now().toString(36);
    const wsName = `pw-proj-${runId}`;
    const subdir = `projects/qa-${runId}`;

    // 1. Login
    await page.goto('http://127.0.0.1:18888/login', { waitUntil: 'networkidle' });
    await page.waitForSelector('input[type="password"]', { timeout: 20000 });
    await page.fill('input[type="text"]', 'alice');
    await page.fill('input[type="password"]', 'alicepass1');
    await page.click('button[type="submit"]');
    await page.waitForTimeout(3000);
    await page.waitForSelector('button:has-text("proj")', { timeout: 20000 });

    // 2. Open workspace dropdown → New Workspace modal
    await page.click('button:has-text("proj")');
    await page.waitForSelector('text=新建工作区', { timeout: 15000 });
    await page.click('text=新建工作区');
    await page.waitForSelector('input[placeholder="我的项目"]', { timeout: 15000 });

    // 3. Empty-name preview: server root, no ~/.hotplex
    const emptyPreview = (await page.textContent('text=路径:')) ?? '';
    expect(emptyPreview).toContain('/tmp/hx-ws-e2e/workspaces/alice/');
    expect(emptyPreview).not.toContain('~/.hotplex');

    // 4. Name → preview root/name-segment
    await page.fill('input[placeholder="我的项目"]', wsName);
    await page.waitForTimeout(300);
    const filledPreview = (await page.textContent('text=路径:')) ?? '';
    expect(filledPreview).toContain(`/tmp/hx-ws-e2e/workspaces/alice/${wsName}`);

    // 5. Multi-level subdir hint placeholder + preview
    const subdirPlaceholder = await page.getAttribute('input[placeholder*="projects/myapp"]', 'placeholder');
    expect(subdirPlaceholder).toContain('projects/myapp');
    await page.fill('input[placeholder*="projects/myapp"]', subdir);
    await page.waitForTimeout(300);
    const multiPreview = (await page.textContent('text=路径:')) ?? '';
    expect(multiPreview).toContain(`/tmp/hx-ws-e2e/workspaces/alice/${subdir}`);

    // 6. Submit → created and visible
    await page.click('button[type="submit"]');
    await page.waitForSelector(`text=${wsName}`, { timeout: 20000 });

    // 7. API surface: stored work_dir + workspace_root
    const listRes = await page.evaluate(async () => {
      const res = await fetch('/api/workspaces', { credentials: 'same-origin' });
      return res.json();
    });
    const ws = listRes.workspaces.find((w: { name: string }) => w.name === wsName);
    expect(ws).toBeTruthy();
    expect(ws.work_dir).toBe(`/tmp/hx-ws-e2e/workspaces/alice/${subdir}`);
    expect(listRes.workspace_root).toBe('/tmp/hx-ws-e2e/workspaces/alice');
  });
});
