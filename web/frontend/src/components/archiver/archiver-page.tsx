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
  distill?: { max_input_tokens?: number; model_name?: string; max_retries?: number }
  push?: { warn_after_consecutive_failures?: number }
  tools_readonly_enabled?: boolean
}

interface ArchiverStatus {
  running: boolean
  last_distilled_at?: string
  last_pushed_at?: string
  consecutive_push_failures?: number
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
  if (res.status === 503) throw new Error("Archiver runner not available (gateway not running?)")
  throw new Error(await res.text())
}

export function ArchiverPage() {
  const [cfg, setCfg] = useState<ArchiverConfig | null>(null)
  const [status, setStatus] = useState<ArchiverStatus | null>(null)
  const [saving, setSaving] = useState(false)
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
      })
      .catch((e) => setError(String(e)))

    const refreshStatus = () =>
      getStatus()
        .then(setStatus)
        .catch(() => {})
    refreshStatus()
    const id = setInterval(refreshStatus, 5000)
    return () => clearInterval(id)
  }, [])

  if (!cfg) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader title="Archiver" />
        <div className="p-6 text-sm text-muted-foreground">
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
    try {
      await runNow()
      setInfo("Triggered. Watch the status panel.")
    } catch (e) {
      setError(String(e))
    }
  }

  return (
    <div className="flex h-full flex-col">
      <PageHeader title="Archiver" />

      <div className="min-h-0 flex-1 overflow-y-auto px-4 sm:px-6">
        <p className="text-muted-foreground pt-2 text-sm">
          Save chat conversations to a git repository so anyone can catch up.
          The archiver writes raw jsonl on every message and runs an LLM
          summarization pass once a day. Disabled when no repository path is set.
        </p>

        {error && (
          <div className="text-destructive bg-destructive/10 mt-4 rounded-lg px-4 py-3 text-sm">
            {error}
          </div>
        )}
        {info && (
          <div className="mt-4 rounded-lg border bg-muted px-4 py-3 text-sm">
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
              One {"<platform>/<chat_id>"} per line. Empty = nothing is archived.
            </Label>
            <Textarea
              id="allow"
              value={cfg.allowlist.join("\n")}
              rows={5}
              placeholder="slack/C0123ABC&#10;pico/main"
              onChange={(e) =>
                setCfg({
                  ...cfg,
                  allowlist: e.target.value.split("\n").filter(Boolean),
                })
              }
              onBlur={() => save(cfg)}
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
                <div>Running: {String(status.running)}</div>
                {status.last_distilled_at && (
                  <div>Last distill: {status.last_distilled_at}</div>
                )}
                {status.last_pushed_at && (
                  <div>Last push: {status.last_pushed_at}</div>
                )}
                {(status.consecutive_push_failures ?? 0) > 0 && (
                  <div className="text-destructive">
                    Push failures: {status.consecutive_push_failures}
                  </div>
                )}
              </div>
            ) : (
              <div className="text-muted-foreground text-sm">No status yet.</div>
            )}
            <div className="flex gap-2">
              <Button onClick={handleRun} disabled={status?.running}>
                Run now
              </Button>
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
