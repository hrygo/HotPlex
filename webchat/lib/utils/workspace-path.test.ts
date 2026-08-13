import { describe, expect, it } from 'vitest';
import {
  workspaceSandboxPrefix,
  buildWorkspaceWorkDir,
  sanitizeWorkspaceDirMulti,
  resolveSandboxAnchor,
  rejoinWorkDir,
} from './workspace-path';

describe('workspaceSandboxPrefix', () => {
  it('appends a trailing slash when missing', () => {
    expect(workspaceSandboxPrefix('/data/hotplex/workspaces/alice')).toBe('/data/hotplex/workspaces/alice/');
  });

  it('keeps an existing trailing slash', () => {
    expect(workspaceSandboxPrefix('/data/hotplex/workspaces/alice/')).toBe('/data/hotplex/workspaces/alice/');
  });

  it('handles a root ending in slash', () => {
    expect(workspaceSandboxPrefix('/x/')).toBe('/x/');
  });
});

describe('sanitizeWorkspaceDirMulti', () => {
  it('keeps multi-level segments', () => {
    expect(sanitizeWorkspaceDirMulti('projects/myapp')).toBe('projects/myapp');
  });

  it('sanitizes each segment independently', () => {
    expect(sanitizeWorkspaceDirMulti('My Project/Sub_Dir')).toBe('my-project/sub-dir');
  });

  it('folds empty segments', () => {
    expect(sanitizeWorkspaceDirMulti('a//b')).toBe('a/b');
    expect(sanitizeWorkspaceDirMulti('/a/')).toBe('a');
  });

  it('drops traversal segments', () => {
    expect(sanitizeWorkspaceDirMulti('projects/../etc')).toBe('projects/etc');
    expect(sanitizeWorkspaceDirMulti('../')).toBe('workspace');
  });

  it('falls back to workspace when empty', () => {
    expect(sanitizeWorkspaceDirMulti('')).toBe('workspace');
    expect(sanitizeWorkspaceDirMulti('   ')).toBe('workspace');
  });
});

describe('buildWorkspaceWorkDir', () => {
  it('builds from the server-provided root with a subdir', () => {
    expect(buildWorkspaceWorkDir('/data/hotplex/workspaces/alice', 'Proj', 'projects/myapp')).toBe(
      '/data/hotplex/workspaces/alice/projects/myapp',
    );
  });

  it('builds from the root with the name segment when subdir is empty', () => {
    expect(buildWorkspaceWorkDir('/data/hotplex/workspaces/alice', 'My Project')).toBe(
      '/data/hotplex/workspaces/alice/my-project',
    );
  });

  it('normalizes a trailing slash on the root', () => {
    expect(buildWorkspaceWorkDir('/data/hotplex/workspaces/alice/', 'Proj')).toBe(
      '/data/hotplex/workspaces/alice/proj',
    );
  });
});

describe('rejoinWorkDir', () => {
  it('rejoins a subdir segment under a trailing-slash prefix', () => {
    expect(rejoinWorkDir('/data/hotplex/workspaces/alice/', 'proj')).toBe('/data/hotplex/workspaces/alice/proj');
  });

  it('preserves multi-level segments (F4 fix)', () => {
    expect(rejoinWorkDir('/data/hotplex/workspaces/alice/', 'projects/myapp')).toBe(
      '/data/hotplex/workspaces/alice/projects/myapp',
    );
  });

  it('returns the bare root for an empty segment (F3 fix)', () => {
    expect(rejoinWorkDir('/data/hotplex/workspaces/alice', '')).toBe('/data/hotplex/workspaces/alice');
    expect(rejoinWorkDir('/data/hotplex/workspaces/alice/', '   ')).toBe('/data/hotplex/workspaces/alice');
  });

  it('inserts exactly one separator when the root has no trailing slash', () => {
    expect(rejoinWorkDir('/data/hotplex/workspaces/alice', 'proj')).toBe('/data/hotplex/workspaces/alice/proj');
  });

  it('sanitizes each edited segment', () => {
    expect(rejoinWorkDir('/data/hotplex/workspaces/alice/', 'My App')).toBe('/data/hotplex/workspaces/alice/my-app');
  });
});

describe('resolveSandboxAnchor', () => {
  it('resolves a username segment root', () => {
    expect(resolveSandboxAnchor('/data/hotplex/workspaces/alice/proj')).toEqual({
      prefix: '/data/hotplex/workspaces/alice/',
      seg: 'proj',
    });
  });

  it('resolves a UUID grandfather root', () => {
    const uuid = '11111111-2222-3333-4444-555555555555';
    expect(resolveSandboxAnchor(`/home/u/.hotplex/workspaces/${uuid}/proj`)).toEqual({
      prefix: `/home/u/.hotplex/workspaces/${uuid}/`,
      seg: 'proj',
    });
  });

  it('treats the sandbox root itself as an empty segment', () => {
    expect(resolveSandboxAnchor('/data/hotplex/workspaces/alice')).toEqual({
      prefix: '/data/hotplex/workspaces/alice',
      seg: '',
    });
  });

  it('returns null when the path has no workspaces/ anchor', () => {
    expect(resolveSandboxAnchor('/tmp/elsewhere')).toBeNull();
    expect(resolveSandboxAnchor('')).toBeNull();
  });

  it('uses the LAST workspaces/ anchor when a prefix interferes', () => {
    expect(resolveSandboxAnchor('/data/workspaces/foo/alice/workspaces/alice/proj')).toEqual({
      prefix: '/data/workspaces/foo/alice/workspaces/alice/',
      seg: 'proj',
    });
  });

  it('resolves a multi-level subdir under the anchor', () => {
    expect(resolveSandboxAnchor('/data/hotplex/workspaces/alice/projects/myapp')).toEqual({
      prefix: '/data/hotplex/workspaces/alice/',
      seg: 'projects/myapp',
    });
  });
});
