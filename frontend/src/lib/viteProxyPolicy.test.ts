import { describe, expect, it } from 'vitest'

import { isLiveEventsRequest } from '../../viteProxyPolicy'

describe('isLiveEventsRequest', () => {
  it('matches the live SSE route with or without a query string', () => {
    expect(isLiveEventsRequest('/api/events')).toBe(true)
    expect(isLiveEventsRequest('/api/events?cursor=12')).toBe(true)
  })

  it('does not hide failures from normal API routes or lookalikes', () => {
    expect(isLiveEventsRequest('/api/customers')).toBe(false)
    expect(isLiveEventsRequest('/api/events/42')).toBe(false)
    expect(isLiveEventsRequest('/api/events-extra')).toBe(false)
    expect(isLiveEventsRequest(undefined)).toBe(false)
  })
})
