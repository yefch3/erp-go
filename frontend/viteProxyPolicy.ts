/**
 * Only the optional SSE endpoint may degrade to an empty response while the
 * local gateway is starting. Keep matching exact so failures from real REST
 * endpoints are never hidden by the development proxy.
 */
export function isLiveEventsRequest(rawURL: string | undefined): boolean {
  if (!rawURL) return false
  try {
    return new URL(rawURL, 'http://vite.local').pathname === '/api/events'
  } catch {
    return false
  }
}
