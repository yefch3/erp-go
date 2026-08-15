import { ref } from 'vue'

// One Server-Sent Events stream per browser tab, shared by every page.
//
// Still `fetch` rather than `EventSource`, though the original reason (an
// Authorization header EventSource cannot set) dissolved when the session
// moved into an httpOnly cookie that rides on either. fetch keeps the
// explicit AbortController lifecycle this file is built around, and
// changing transports to remove a comment is not a trade.

export interface LiveEvent {
  type: string
  subject?: string
  at: string
}

type Handler = (event: LiveEvent) => void

const handlers = new Set<Handler>()
let controller: AbortController | null = null
let retryDelay = 1000

/** True while the stream is up; pages can show a "live" indicator from it. */
export const connected = ref(false)

/** Subscribe to live hints. Returns an unsubscribe function. */
export function onLive(handler: Handler): () => void {
  handlers.add(handler)
  return () => handlers.delete(handler)
}

export function startLive(): void {
  if (controller) return
  controller = new AbortController()
  void run(controller.signal)
}

export function stopLive(): void {
  controller?.abort()
  controller = null
  connected.value = false
}

async function run(signal: AbortSignal): Promise<void> {
  while (!signal.aborted) {
    try {
      // The signed-in signal, not the credential — the cookie goes with the
      // request on its own. Bailing out while signed out keeps a logged-out
      // tab from hammering /api/events into a wall of 401s.
      if (!localStorage.getItem('employeeName')) return
      const resp = await fetch('/api/events', { signal })
      if (!resp.ok || !resp.body) throw new Error(`stream failed: ${resp.status}`)
      connected.value = true
      retryDelay = 1000
      await consume(resp.body, signal)
    } catch {
      // Network blips and server restarts are normal; the page keeps working
      // without the stream, it just goes back to refreshing by hand.
      connected.value = false
    }
    if (signal.aborted) return
    await sleep(retryDelay)
    // Back off to a minute so a server that stays down is not hammered.
    retryDelay = Math.min(retryDelay * 2, 60_000)
  }
}

async function consume(body: ReadableStream<Uint8Array>, signal: AbortSignal): Promise<void> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (!signal.aborted) {
    const { done, value } = await reader.read()
    if (done) return
    buffer += decoder.decode(value, { stream: true })
    // SSE frames are separated by a blank line; anything after the last one
    // is a partial frame and stays in the buffer.
    const frames = buffer.split('\n\n')
    buffer = frames.pop() ?? ''
    for (const frame of frames) dispatch(frame)
  }
}

function dispatch(frame: string): void {
  const data = frame
    .split('\n')
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice(5).trim())
    .join('')
  if (!data) return // heartbeat comment, or a frame with no payload
  try {
    const event = JSON.parse(data) as LiveEvent
    handlers.forEach((handler) => handler(event))
  } catch {
    // A malformed hint is dropped; the next refresh has the truth anyway.
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
