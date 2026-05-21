import { AdminShell } from './admin-shell';
import { AdminUIProvider } from '@/context/admin-ui-context';

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  return (
    <AdminUIProvider>
      <AdminShell>{children}</AdminShell>
    </AdminUIProvider>
  );
}
