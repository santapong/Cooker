import { describe, it, expect } from 'vitest'
import { pipelineYamlFilename } from './download'

describe('pipelineYamlFilename', () => {
  it('slugifies a normal name', () => {
    expect(pipelineYamlFilename('My Release Pipeline')).toBe('My-Release-Pipeline.cooker.yaml')
  })

  it('keeps safe characters', () => {
    expect(pipelineYamlFilename('build_push.deploy-1')).toBe('build_push.deploy-1.cooker.yaml')
  })

  it('collapses runs of unsafe characters to a single hyphen', () => {
    expect(pipelineYamlFilename('a   //  b')).toBe('a-b.cooker.yaml')
  })

  it('trims leading and trailing separators', () => {
    expect(pipelineYamlFilename('  spaced  ')).toBe('spaced.cooker.yaml')
    expect(pipelineYamlFilename('...dots...')).toBe('dots.cooker.yaml')
  })

  it('falls back to "pipeline" for an empty or all-symbol name', () => {
    expect(pipelineYamlFilename('')).toBe('pipeline.cooker.yaml')
    expect(pipelineYamlFilename('@@@')).toBe('pipeline.cooker.yaml')
  })

  it('mirrors the backend suffix so downloads name consistently', () => {
    // The backend uses the same ".cooker.yaml" suffix in its
    // Content-Disposition header (handler.pipelineYAMLFilename).
    expect(pipelineYamlFilename('release')).toMatch(/\.cooker\.yaml$/)
  })
})
