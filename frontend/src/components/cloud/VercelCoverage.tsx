import type { VercelConnection } from '../../types/vercel';
import { timeAgo } from '../../utils/time';

export default function VercelCoverage({ connection }: { connection: VercelConnection }) {
  const coverage = connection.coverage;
  const valid = coverage && Number.isInteger(coverage.accessibleProjects) && Number.isInteger(coverage.selectedProjects)
    && coverage.selectedProjects >= 0 && coverage.accessibleProjects >= coverage.selectedProjects
    && Number.isFinite(Date.parse(coverage.observedAt));
  return <div className="vercel-stack">
    {valid ? <p className="vercel-meta" title={coverage.observedAt}>
      Last discovery: {coverage.selectedProjects} selected of {coverage.accessibleProjects} accessible projects
      {' · '}{coverage.accessibleProjects - coverage.selectedProjects} not selected
      {' · '}{timeAgo(coverage.observedAt)}
    </p> : <p className="vercel-hint">Discovery coverage is unavailable until a successful sync. Manage projects to check current access.</p>}
    <p className="vercel-hint">Coverage applies to this account or team and this connection’s permissions. New projects require selection; other accounts and teams need their own connection.</p>
  </div>;
}
