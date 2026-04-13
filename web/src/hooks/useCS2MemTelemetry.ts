import { useState, useEffect } from 'react'
import { useWebSocket } from './useWebSocket'

/** Снимок с каждого тика бота (~target_hz Hz), событие WS `cs2:mem`. */
export type CS2MemPayload = {
  display: number
  bot_tick: number
  ts_ms: number
  target_hz: number
  mem_poll_ran: boolean
  phase: string
  autoplay_live: boolean
  mem_driver: boolean
  mem_poll_ok: boolean
  mem_poll_err: string
  mem_poll_age_ms: number | null
  snapshot_fail_streak: number
  mem_nav_active: boolean
  esp_box_count: number
  nav_route_len: number
  nav_route_step: number
  yolo: boolean
  mem_yaw_deg?: number
  mem_fresh_ms?: number
  world_x?: number
  world_y?: number
  world_z?: number
  map?: string
}

/** Последний снимок по каждому display (обновляется на каждое сообщение). */
export function useCS2MemLatestByDisplay() {
  const [byDisp, setByDisp] = useState<Record<number, CS2MemPayload>>({})
  const { subscribe } = useWebSocket()

  useEffect(() => {
    return subscribe((msg) => {
      if (msg.type === 'cs2:mem') {
        const p = msg.payload as CS2MemPayload
        if (p?.display != null) {
          setByDisp((prev) => ({ ...prev, [p.display]: p }))
        }
      }
    })
  }, [subscribe])

  return byDisp
}
