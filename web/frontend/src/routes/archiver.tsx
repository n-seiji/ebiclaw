import { createFileRoute } from "@tanstack/react-router"

import { ArchiverPage } from "@/components/archiver/archiver-page"

export const Route = createFileRoute("/archiver")({
  component: ArchiverPage,
})
