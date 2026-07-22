import { useEffect, useState } from "react"

import { PageHeader } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"

interface ArchiverConfig {
  enabled: boolean
  repository_path: string
  allowlist: string[]
  schedule: { cron: string; timezone: string }
  distill?: {
    max_input_tokens?: number
    model_name?: string
    max_retries?: number
  }
  push?: { warn_after_consecutive_failures?: number }
  tools_readonly_enabled?: boolean
}

interface ArchiverStatus {
  running: boolean
  service_running?: boolean
  run_in_progress?: boolean
  last_distilled_at?: string
  last_pushed_at?: string
  consecutive_push_failures?: number
  logs?: ArchiverLogEntry[]
}

interface ArchiverLogEntry {
  time: string
  level: "info" | "warn" | "error" | string
  message: string
  fields?: Record<string, unknown>
}

async function getConfig(): Promise<ArchiverConfig> {
  const res = await fetch("/api/archiver/config")
  if (!res.ok) throw new Error(`failed to load: ${res.status}`)
  return res.json()
}

async function putConfig(c: ArchiverConfig): Promise<void> {
  const res = await fetch("/api/archiver/config", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(c),
  })
  if (!res.ok) throw new Error(await res.text())
}

async function getStatus(): Promise<ArchiverStatus> {
  const res = await fetch("/api/archiver/status")
  if (!res.ok) throw new Error("failed")
  return res.json()
}

async function runNow(): Promise<void> {
  const res = await fetch("/api/archiver/run", { method: "POST" })
  if (res.ok) return
  if (res.status === 409) throw new Error("Archiver is busy")
  if (res.status === 503) {
    const message = await responseErrorMessage(res)
    throw new Error(
      message || "Archiver runner not available (gateway not running?)",
    )
  }
  throw new Error(await responseErrorMessage(res))
}

async function responseErrorMessage(res: Response): Promise<string> {
  const text = await res.text()
  if (!text) return `Request failed: ${res.status}`
  try {
    const body = JSON.parse(text) as { error?: unknown }
    if (typeof body.error === "string") return body.error
  } catch {
    // Fall back to the raw response body below.
  }
  return text
}

export function ArchiverPage() {
  const [cfg, setCfg] = useState<ArchiverConfig | null>(null)
  const [allowlistText, setAllowlistText] = useState("")
  const [status, setStatus] = useState<ArchiverStatus | null>(null)
  const [saving, setSaving] = useState(false)
  const [runningNow, setRunningNow] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [info, setInfo] = useState<string | null>(null)

  useEffect(() => {
    getConfig()
      .then((c) => {
        // ensure shape (server may omit fields)
        setCfg({
          enabled: c.enabled ?? false,
          repository_path: c.repository_path ?? "",
          allowlist: c.allowlist ?? [],
          schedule: {
            cron: c.schedule?.cron ?? "0 3 * * *",
            timezone: c.schedule?.timezone ?? "Asia/Tokyo",
          },
          distill: c.distill,
          push: c.push,
          tools_readonly_enabled: c.tools_readonly_enabled ?? false,
        })
        setAllowlistText((c.allowlist ?? []).join("\n"))
      })
      .catch((e) => setError(String(e)))

    getStatus()
      .then(setStatus)
      .catch(() => {})
  }, [])

  if (!cfg) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader title="Archiver" />
        <div className="text-muted-foreground p-6 text-sm">
          {error ?? "Loading…"}
        </div>
      </div>
    )
  }

  const save = async (next: ArchiverConfig) => {
    setSaving(true)
    setError(null)
    setInfo(null)
    try {
      await putConfig(next)
      setCfg(next)
      setInfo("Saved.")
    } catch (e) {
      setError(String(e))
    } finally {
      setSaving(false)
    }
  }

  const handleRun = async () => {
    setError(null)
    setInfo(null)
    setRunningNow(true)
    try {
      await runNow()
      setInfo("Triggered.")
    } catch (e) {
      setError(String(e))
    } finally {
      setRunningNow(false)
    }
  }

  const parseAllowlist = (text: string): string[] =>
    text
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter(Boolean)

  return (
    <div className="flex h-full flex-col">
      <PageHeader title="Archiver" />

      <div className="min-h-0 flex-1 overflow-y-auto px-4 sm:px-6">
        <p className="text-muted-foreground pt-2 text-sm">
          Save chat conversations to a git repository so anyone can catch up.
          The archiver writes raw jsonl on every message and runs an LLM
          summarization pass once a day. Disabled when no repository path is
          set.
        </p>

        {error && (
          <div className="text-destructive bg-destructive/10 mt-4 rounded-lg px-4 py-3 text-sm">
            {error}
          </div>
        )}
        {info && (
          <div className="bg-muted mt-4 rounded-lg border px-4 py-3 text-sm">
            {info}
          </div>
        )}

        <Card className="mt-4">
          <CardHeader>
            <CardTitle>Repository</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <Label htmlFor="enabled">Enabled</Label>
              <Switch
                id="enabled"
                checked={cfg.enabled}
                onCheckedChange={(v) => save({ ...cfg, enabled: v })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="repo">Repository path</Label>
              <Input
                id="repo"
                value={cfg.repository_path}
                placeholder="/path/to/local/git/working/tree"
                onChange={(e) =>
                  setCfg({ ...cfg, repository_path: e.target.value })
                }
                onBlur={() => save(cfg)}
              />
              <p className="text-muted-foreground text-xs">
                Must be an existing local git working tree with a remote
                configured. The archiver runs git add / commit / push from this
                directory.
              </p>
            </div>
          </CardContent>
        </Card>

        <Card className="mt-4">
          <CardHeader>
            <CardTitle>Allowlist</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <Label htmlFor="allow">
              One {"<platform>/<chat_id>"} per line. Empty = nothing is
              archived.
            </Label>
            <Textarea
              id="allow"
              value={allowlistText}
              rows={5}
              placeholder="slack/C0123ABC&#10;pico/main"
              onChange={(e) => setAllowlistText(e.target.value)}
              onBlur={() => {
                const next = {
                  ...cfg,
                  allowlist: parseAllowlist(allowlistText),
                }
                setCfg(next)
                void save(next)
              }}
            />
          </CardContent>
        </Card>

        <Card className="mt-4">
          <CardHeader>
            <CardTitle>Schedule</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="cron">Cron</Label>
                <Input
                  id="cron"
                  value={cfg.schedule.cron}
                  onChange={(e) =>
                    setCfg({
                      ...cfg,
                      schedule: { ...cfg.schedule, cron: e.target.value },
                    })
                  }
                  onBlur={() => save(cfg)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="tz">Timezone</Label>
                <Input
                  id="tz"
                  value={cfg.schedule.timezone}
                  onChange={(e) =>
                    setCfg({
                      ...cfg,
                      schedule: {
                        ...cfg.schedule,
                        timezone: e.target.value,
                      },
                    })
                  }
                  onBlur={() => save(cfg)}
                />
              </div>
            </div>
            <p className="text-muted-foreground text-xs">
              Default {`"0 3 * * *"`} runs at 03:00 in the configured timezone.
            </p>
          </CardContent>
        </Card>

        <Card className="mt-4">
          <CardHeader>
            <CardTitle>Tools</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center justify-between">
              <div>
                <Label htmlFor="tools">
                  Expose archive_search / archive_read to Pico
                </Label>
                <p className="text-muted-foreground text-xs">
                  When on, Pico can read topics from the archive when answering.
                </p>
              </div>
              <Switch
                id="tools"
                checked={cfg.tools_readonly_enabled ?? false}
                onCheckedChange={(v) =>
                  save({ ...cfg, tools_readonly_enabled: v })
                }
              />
            </div>
          </CardContent>
        </Card>

        <Card className="mt-4 mb-6">
          <CardHeader>
            <CardTitle>Status</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {status ? (
              <div className="text-sm">
                <div>
                  Service running:{" "}
                  {String(status.service_running ?? status.running)}
                </div>
                <div>
                  Run in progress: {String(status.run_in_progress ?? false)}
                </div>
                {isMeaningfulTimestamp(status.last_distilled_at) && (
                  <div>Last distill: {status.last_distilled_at}</div>
                )}
                {isMeaningfulTimestamp(status.last_pushed_at) && (
                  <div>Last push: {status.last_pushed_at}</div>
                )}
                {(status.consecutive_push_failures ?? 0) > 0 && (
                  <div className="text-destructive">
                    Push failures: {status.consecutive_push_failures}
                  </div>
                )}
              </div>
            ) : (
              <div className="text-muted-foreground text-sm">
                No status yet.
              </div>
            )}
            <div className="space-y-2">
              <div className="text-sm font-medium">Logs</div>
              {status?.logs?.length ? (
                <div className="bg-muted/30 max-h-80 overflow-y-auto rounded-md border">
                  {status.logs
                    .slice()
                    .reverse()
                    .map((log, i) => (
                      <div
                        key={`${log.time}-${i}`}
                        className="border-b px-3 py-2 font-mono text-xs last:border-b-0"
                      >
                        <div className="flex flex-wrap gap-x-2 gap-y-1">
                          <span className="text-muted-foreground">
                            {formatLogTime(log.time)}
                          </span>
                          <span className={logLevelClass(log.level)}>
                            {log.level}
                          </span>
                          <span>{log.message}</span>
                        </div>
                        {log.fields && Object.keys(log.fields).length > 0 && (
                          <pre className="text-muted-foreground mt-1 break-words whitespace-pre-wrap">
                            {JSON.stringify(log.fields, null, 2)}
                          </pre>
                        )}
                      </div>
                    ))}
                </div>
              ) : (
                <div className="text-muted-foreground rounded-md border px-3 py-2 text-sm">
                  No archiver logs yet.
                </div>
              )}
            </div>
            <div className="flex gap-2">
              <Button onClick={handleRun} disabled={runningNow}>
                Run now
              </Button>
              {runningNow && (
                <span className="text-muted-foreground text-sm">Running…</span>
              )}
              {saving && (
                <span className="text-muted-foreground text-sm">Saving…</span>
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function formatLogTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function isMeaningfulTimestamp(value: string | undefined): value is string {
  return Boolean(value && !value.startsWith("0001-01-01"))
}

function logLevelClass(level: string): string {
  if (level === "error") return "text-destructive"
  if (level === "warn") return "text-amber-600"
  return "text-muted-foreground"
}
