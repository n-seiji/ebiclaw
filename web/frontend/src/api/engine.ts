import { launcherFetch } from "@/api/http"

// API client for CLI engine (codex / claude-code) selection.

export type EngineSandbox = "read-only" | "workspace-write" | "danger-full-access"

export interface EngineBackendInfo {
  id: string
  available: boolean
}

export interface EngineInfo {
  backend: string
  model: string
  workspace: string
  sandbox: EngineSandbox
  available_backends: EngineBackendInfo[]
  chat_ready: boolean
}

export interface EngineUpdatePayload {
  backend?: string
  model?: string
  workspace?: string
  sandbox?: EngineSandbox
}

interface EngineActionResponse {
  status: string
}

const BASE_URL = ""

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await launcherFetch(`${BASE_URL}${path}`, options)
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json() as Promise<T>
}

export async function getEngine(): Promise<EngineInfo> {
  return request<EngineInfo>("/api/engine")
}

export async function updateEngine(
  payload: EngineUpdatePayload,
): Promise<EngineActionResponse> {
  return request<EngineActionResponse>("/api/engine", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  })
}
