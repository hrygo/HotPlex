'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';

// Members management has moved to /settings?tab=members (issue #788 A3). The
// ability is unchanged — only the surface was duplicated here. This route is
// kept as a permanent redirect for existing bookmarks and nav links.
export default function MembersRedirectPage() {
  const router = useRouter();
  useEffect(() => {
    router.replace('/settings?tab=members');
  }, [router]);
  return null;
}
