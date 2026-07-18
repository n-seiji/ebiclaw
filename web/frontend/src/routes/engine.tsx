import { createFileRoute } from "@tanstack/react-router"

import { EnginePage } from "@/components/engine/engine-page"

export const Route = createFileRoute("/engine")({
  component: EnginePage,
})
