export function formatDuration(ms: number): string {
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return sec % 60 ? `${min}m${sec % 60}s` : `${min}m`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return min % 60 ? `${hr}h${min % 60}m` : `${hr}h`;
  const d = Math.floor(hr / 24);
  return hr % 24 ? `${d}d${hr % 24}h` : `${d}d`;
}
