import type { PipelineRun } from '../../types/pipeline';

export default function RunSourceReceipt({ run, currentVersion }: { run: PipelineRun; currentVersion?: number }) {
  const source = run.source;
  const checkout = source?.checkedOutAt ? new Date(source.checkedOutAt) : null;
  const verifiedCheckout = checkout && Number.isFinite(checkout.getTime());
  const changed = !!run.pipelineVersion && !!currentVersion && run.pipelineVersion !== currentVersion;
  return <details className="run-source-receipt">
    <summary>Source receipt · {source ? `${source.source.repository} @ ${source.commitSha.slice(0, 12)}` : 'Unavailable'}</summary>
    {source ? <>
      <p>{verifiedCheckout ? 'The worker verified checkout of this accepted commit.' : 'This commit was accepted for the run. Verified checkout has not been recorded.'}</p>
      <dl>
        <dt>Provider</dt><dd>{source.source.provider} · {source.source.host}</dd>
        <dt>Repository</dt><dd>{source.source.repository}</dd>
        <dt>Ref</dt><dd>{source.fullRef}</dd>
        <dt>Commit</dt><dd>{source.commitSha}</dd>
        {source.objectSha && <><dt>Ref object</dt><dd>{source.objectSha}</dd></>}
        <dt>Accepted source plan</dt><dd>{source.sourcePlanDigest}</dd>
        <dt>Verified checkout</dt><dd>{verifiedCheckout ? checkout.toLocaleString() : 'Not recorded'}</dd>
      </dl>
    </> : <p>No connected Git source receipt is available for this run. Cooker does not infer its source from the current pipeline.</p>}
    <p>Accepted pipeline version: {run.pipelineVersion || 'Unavailable'}{changed && ` · Current version: ${currentVersion}`}</p>
    {changed && <p role="status">The pipeline was edited after this run was accepted. The graph below shows the current definition; it does not reconstruct the accepted graph.</p>}
  </details>;
}
