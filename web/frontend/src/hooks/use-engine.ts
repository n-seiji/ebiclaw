import { useCallback, useEffect, useState } from "react"

import { type EngineInfo, getEngine } from "@/api/engine"

interface UseEngineOptions {
  isConnected: boolean
}

export function useEngine({ isConnected }: UseEngineOptions) {
  const [engine, setEngine] = useState<EngineInfo | null>(null)

  const loadEngine = useCallback(async () => {
    try {
      const data = await getEngine()
      setEngine(data)
    } catch {
      // silently fail; chatReady defaults to false until a successful load
    }
  }, [])

  useEffect(() => {
    const timerId = setTimeout(() => {
      void loadEngine()
    }, 0)

    return () => clearTimeout(timerId)
  }, [isConnected, loadEngine])

  return {
    engine,
    chatReady: engine?.chat_ready ?? false,
    reloadEngine: loadEngine,
  }
}
