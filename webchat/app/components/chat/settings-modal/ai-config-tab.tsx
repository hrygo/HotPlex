'use client';

import { type Workspace } from '@/lib/api/workspaces';
import { AgentConfigEditor } from '@/components/admin/agent-config-editor';
import { TabPanel } from './tab-panel';

interface AIConfigTabProps {
  workspace: Workspace;
  onUpdated?: (ws: Workspace) => void;
}

export function AIConfigTab({ workspace, onUpdated }: AIConfigTabProps) {
  return (
    <TabPanel>
      <AgentConfigEditor
        workspaceId={workspace.id}
        overrides={workspace.agent_config_overrides || {}}
        onSaved={onUpdated}
      />
    </TabPanel>
  );
}
