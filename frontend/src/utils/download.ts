/**
 * downloadText triggers a browser download of an in-memory string as a
 * file. Used by pipeline-as-code export to save the YAML document the
 * backend returned without a second round-trip. It creates a transient
 * object URL, clicks a synthetic anchor, then revokes the URL.
 */
export function downloadText(filename: string, contents: string, mime = 'application/yaml'): void {
  const blob = new Blob([contents], { type: `${mime};charset=utf-8` })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

/**
 * pipelineYamlFilename derives a filesystem-safe "<slug>.cooker.yaml"
 * name from a pipeline name, mirroring the backend's Content-Disposition
 * suggestion so a download names the file the same way whether the
 * browser honours the header or the frontend falls back to this. Empty
 * or all-symbol names collapse to "pipeline".
 */
export function pipelineYamlFilename(name: string): string {
  const slug = name
    .replace(/[^a-zA-Z0-9._-]+/g, '-')
    .replace(/^[-.]+|[-.]+$/g, '')
    .slice(0, 80)
    .replace(/^[-.]+|[-.]+$/g, '')
  return `${slug || 'pipeline'}.cooker.yaml`
}
