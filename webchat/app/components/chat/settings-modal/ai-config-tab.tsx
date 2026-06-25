'use client';

import { type Workspace } from '@/lib/api/workspaces';
import { AgentConfigEditor } from '@/components/admin/agent-config-editor';
import { TabPanel } from './tab-panel';

interface AIConfigTabProps {
  workspace: Workspace;
}

export function AIConfigTab({ workspace }: AIConfigTabProps) {
  return (
    <TabPanel>
      <AgentConfigEditor workspaceId={workspace.id} overrides={workspace.agent_config_overrides || {}} />
    </TabPanel>
  );
}
