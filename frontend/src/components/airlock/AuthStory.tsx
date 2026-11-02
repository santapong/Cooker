/** An illustrative pipeline, deliberately separate from real deployment status. */
export default function AuthStory() {
  return (
    <aside className="auth-story" aria-labelledby="auth-story-heading">
      <div className="auth-story-content">
        <p className="auth-eyebrow">01 / Back to building</p>
        <h2 id="auth-story-heading">Your next deploy.<br /><span>One clear path.</span></h2>
        <p className="auth-story-description">Source, checks, and the next decision.<br />Right where you can see them.</p>
        <div className="auth-pipeline" aria-label="Example pipeline: build, review, then deploy">
          <div className="auth-pipeline-bar">
            <span><span className="auth-branch-mark" aria-hidden="true">⑂</span> release / web</span>
            <span>Example pipeline</span>
          </div>
          <ol className="auth-pipeline-flow">
            <li className="auth-pipeline-step">
              <span className="auth-step-icon" aria-hidden="true">
                <svg viewBox="0 0 32 32"><path d="m16 4 11 6v12l-11 6-11-6V10Zm0 12 11-6M16 16 5 10m11 6v12M10.5 7l11 6" /></svg>
              </span>
              <span className="auth-step-number">01</span><strong>Build</strong>
            </li>
            <li className="auth-pipeline-step is-review">
              <span className="auth-step-icon" aria-hidden="true">
                <svg viewBox="0 0 32 32"><path d="m16 3 13 13-13 13L3 16Z" /><path d="m10 16 4 4 8-8" /></svg>
              </span>
              <span className="auth-step-number">02</span><strong>Review</strong>
            </li>
            <li className="auth-pipeline-step is-next">
              <span className="auth-step-icon" aria-hidden="true">
                <svg viewBox="0 0 32 32"><path d="M13 20 9 16C13 7 20 4 28 4c0 8-3 15-12 19l-3-3Zm0 0-6 5m4-10H5l-3 9 7-2m8 0v5l-9 3 2-7" /><circle cx="22" cy="10" r="2" /></svg>
              </span>
              <span className="auth-step-number">03</span><strong>Deploy</strong>
            </li>
          </ol>
          <div className="auth-pipeline-receipt">
            <span className="auth-review-mark" aria-hidden="true">✓</span>
            <div><strong>A moment to make sure.</strong><p>A human checkpoint before deployment.</p></div>
          </div>
        </div>
        <div className="auth-story-foot"><span>Reviewed source</span><span>Your infrastructure</span></div>
      </div>
    </aside>
  );
}
