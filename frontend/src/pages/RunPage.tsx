import { useParams } from 'react-router-dom';
import RunView from '../components/run/RunView';

/** A pipeline run in the porthole — see components/run/RunView. */
export default function RunPage() {
  const { id = '', runId = '' } = useParams<{ id: string; runId: string }>();
  return <RunView pipelineId={id} runId={runId} heading={(p, r) => `Porthole · ${p.name} · run ${r.id.slice(0, 8)}`} />;
}
