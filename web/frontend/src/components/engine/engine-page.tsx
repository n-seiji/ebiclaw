import { IconCheck, IconClock, IconLoader2 } from "@tabler/icons-react"
import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import {
  type EngineInfo,
  type EngineSandbox,
  getEngine,
  updateEngine,
} from "@/api/engine"
import { Field } from "@/components/shared-form"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { PageHeader } from "@/components/page-header"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { cn } from "@/lib/utils"

const SANDBOX_OPTIONS: EngineSandbox[] = [
  "read-only",
  "workspace-write",
  "danger-full-access",
]

function sandboxLabelKey(sandbox: EngineSandbox): string {
  switch (sandbox) {
    case "read-only":
      return "engine.sandbox.options.readOnly"
    case "workspace-write":
      return "engine.sandbox.options.workspaceWrite"
    case "danger-full-access":
      return "engine.sandbox.options.dangerFullAccess"
  }
}

interface BackendOption {
  id: string
  titleKey: string
  descriptionKey: string
  available: boolean
}

export function EnginePage() {
  const { t } = useTranslation()
  const [engine, setEngine] = useState<EngineInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [fetchError, setFetchError] = useState("")
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState("")
  const [dirty, setDirty] = useState(false)

  const [model, setModel] = useState("")
  const [workspace, setWorkspace] = useState("")
  const [sandbox, setSandbox] = useState<EngineSandbox>("workspace-write")

  const applyEngine = useCallback((data: EngineInfo) => {
    setEngine(data)
    setModel(data.model)
    setWorkspace(data.workspace)
    setSandbox(data.sandbox)
    setDirty(false)
  }, [])

  const fetchEngine = useCallback(async () => {
    try {
      const data = await getEngine()
      applyEngine(data)
      setFetchError("")
    } catch (e) {
      setFetchError(e instanceof Error ? e.message : t("engine.loadError"))
    } finally {
      setLoading(false)
    }
  }, [applyEngine, t])

  useEffect(() => {
    fetchEngine()
  }, [fetchEngine])

  const availability = new Map(
    (engine?.available_backends ?? []).map((b) => [b.id, b.available]),
  )

  const backendOptions: BackendOption[] = [
    {
      id: "codex",
      titleKey: "engine.backend.codex.title",
      descriptionKey: "engine.backend.codex.description",
      available: availability.get("codex") ?? false,
    },
    {
      id: "claude-code",
      titleKey: "engine.backend.claudeCode.title",
      descriptionKey: "engine.backend.claudeCode.description",
      available: availability.get("claude-code") ?? false,
    },
  ]

  const handleSave = async () => {
    setSaving(true)
    setSaveError("")
    try {
      await updateEngine({
        backend: "codex",
        model,
        workspace,
        sandbox,
      })
      await fetchEngine()
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : t("engine.saveError"))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex h-full flex-col">
      <PageHeader title={t("engine.title")}>
        <Button size="sm" onClick={handleSave} disabled={saving || !dirty}>
          {saving ? (
            <IconLoader2 className="size-4 animate-spin" />
          ) : (
            <IconCheck className="size-4" />
          )}
          {t("common.save")}
        </Button>
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 sm:px-6">
        <p className="text-muted-foreground mt-2 mb-6 text-sm">
          {t("engine.description")}
        </p>

        {loading && (
          <div className="flex items-center justify-center py-20">
            <IconLoader2 className="text-muted-foreground size-6 animate-spin" />
          </div>
        )}

        {fetchError && (
          <div className="text-destructive bg-destructive/10 rounded-lg px-4 py-3 text-sm">
            {fetchError}
          </div>
        )}

        {!loading && !fetchError && engine && (
          <div className="max-w-2xl pb-8">
            <div className="grid gap-3 sm:grid-cols-2">
              {backendOptions.map((option) => (
                <button
                  key={option.id}
                  type="button"
                  disabled={!option.available}
                  className={cn(
                    "rounded-lg border p-4 text-left transition-colors",
                    option.id === "codex"
                      ? "border-primary bg-primary/5"
                      : "border-border",
                    !option.available && "cursor-not-allowed opacity-60",
                  )}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-medium">{t(option.titleKey)}</span>
                    {!option.available && (
                      <Badge variant="secondary">
                        {t("engine.backend.comingSoon")}
                      </Badge>
                    )}
                  </div>
                  <p className="text-muted-foreground mt-1 text-xs">
                    {t(option.descriptionKey)}
                  </p>
                </button>
              ))}
            </div>

            <div className="mt-6 space-y-4">
              <Field
                label={t("engine.model.label")}
                hint={t("engine.model.hint")}
              >
                <Input
                  value={model}
                  onChange={(e) => {
                    setModel(e.target.value)
                    setDirty(true)
                  }}
                  placeholder={t("engine.model.placeholder")}
                />
              </Field>

              <Field label={t("engine.workspace.label")}>
                <Input
                  value={workspace}
                  onChange={(e) => {
                    setWorkspace(e.target.value)
                    setDirty(true)
                  }}
                  placeholder={t("engine.workspace.placeholder")}
                />
              </Field>

              <Field label={t("engine.sandbox.label")}>
                <Select
                  value={sandbox}
                  onValueChange={(value) => {
                    setSandbox(value as EngineSandbox)
                    setDirty(true)
                  }}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {SANDBOX_OPTIONS.map((option) => (
                      <SelectItem key={option} value={option}>
                        {t(sandboxLabelKey(option))}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>

            {saveError && (
              <div className="text-destructive bg-destructive/10 mt-4 rounded-lg px-4 py-3 text-sm">
                {saveError}
              </div>
            )}

            {dirty && (
              <div className="text-muted-foreground mt-6 flex items-start gap-2 text-xs">
                <IconClock className="mt-0.5 size-3.5 shrink-0" />
                <span>{t("header.gateway.restartRequired")}</span>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
