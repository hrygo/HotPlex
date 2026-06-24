'use client';

import { type Workspace } from '@/lib/api/workspaces';
import { AgentConfigEditor } from '@/components/admin/agent-config-editor';

interface AIConfigTabProps {
  workspace: Workspace;
}

export function AIConfigTab({ workspace }: AIConfigTabProps) {
  return (
    <div className="w-full">
      <AgentConfigEditor workspaceId={workspace.id} overrides={workspace.agent_config_overrides || {}} />
    </div>
  );
}
