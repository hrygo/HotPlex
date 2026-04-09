'use client';

import { MessagePrimitive, ActionBarPrimitive, ComposerPrimitive } from '@assistant-ui/react';
import { SendIcon, EditIcon } from './icons';

export function UserMessage() {
  return (
    <MessagePrimitive.Root
      className="group"
      style={{ display: 'flex', justifyContent: 'flex-end', padding: '0.5rem 0' }}
    >
      <div style={{ maxWidth: '80%', minWidth: 0 }}>
        <div className="user-bubble">
          <MessagePrimitive.Content />
        </div>

        <ActionBarPrimitive.Root className="aui-action-bar aui-action-bar-user opacity-0 group-hover:opacity-100 transition-opacity">
          <ActionBarPrimitive.Copy className="aui-copy-btn" />
          <ActionBarPrimitive.Edit className="aui-action-btn">
            <EditIcon /> Edit
          </ActionBarPrimitive.Edit>
        </ActionBarPrimitive.Root>
      </div>
    </MessagePrimitive.Root>
  );
}

export function EditComposer() {
  return (
    <ComposerPrimitive.Root className="edit-composer-root">
      <ComposerPrimitive.Input
        className="focus-ring min-h-[24px] max-h-[120px]"
        rows={1}
      />
      <ComposerPrimitive.Send className="send-btn send-btn-sm" disabled>
        <SendIcon size={16} />
      </ComposerPrimitive.Send>
    </ComposerPrimitive.Root>
  );
}
