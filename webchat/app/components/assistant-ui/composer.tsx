'use client';

import { ComposerPrimitive } from '@assistant-ui/react';
import { SendIcon, StopIcon } from './icons';

export function Composer() {
  return (
    <ComposerPrimitive.Root className="composer-root">
      <div className="composer-input-row">
        <ComposerPrimitive.Input
          className="focus-ring composer-input"
          rows={1}
        />

        <ComposerPrimitive.Cancel className="cancel-btn" title="Stop">
          <StopIcon />
        </ComposerPrimitive.Cancel>

        <ComposerPrimitive.Send className="send-btn" disabled>
          <SendIcon />
        </ComposerPrimitive.Send>
      </div>
    </ComposerPrimitive.Root>
  );
}
