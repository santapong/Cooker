import type { PipelineSourceConfig } from '../../types/pipeline';

export interface SourceChoice {
  id: string;
  name: string;
  provider: 'github' | 'gitlab' | 'gitea';
  enabled: boolean;
  version: number;
  repositories: string[];
}

/** Local guidance only. The server verifies identity, permissions and revision. */
export function sourceBindingIssue(source: PipelineSourceConfig | null | undefined, connections: readonly SourceChoice[]): string | null {
  if (!source) return null;
  const connection = connections.find((item) => item.id === source.connectionId);
  if (!connection?.enabled) return 'The selected connection is unavailable. Ask an administrator to check it.';
  if (!connection.repositories.includes(source.repository)) return 'Choose a repository allowed by this connection.';
  const ref = source.fullRef;
  const name = ref.replace(/^refs\/(heads|tags)\//, '');
  if (!/^refs\/(heads|tags)\/.+/.test(ref) || ref.length > 1024 || /[\s~^:?*[\\]/.test(ref) || [...ref].some((character) => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127) || ref.includes('..') || ref.includes('@{') || name.split('/').some((part) => !part || part.startsWith('.') || part.endsWith('.lock')) || ref.endsWith('.')) {
    return 'Use an exact full ref, such as refs/heads/main or refs/tags/v1.0.0.';
  }
  return null;
}
