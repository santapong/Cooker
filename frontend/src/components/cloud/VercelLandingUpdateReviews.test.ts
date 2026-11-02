import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { landingUpdateReview, reviewAdoption, reviewArtifact, reviewConnection, reviewNow } from '../../test/fixtures/vercelLandingUpdateReviews';
import { landingUpdateReviewState } from '../../utils/vercelLandingUpdateReviews';
import { LandingUpdateReviewRecord } from './VercelLandingUpdateReviews';

function render(review = landingUpdateReview()) {
  const state = landingUpdateReviewState(review, {
    adoptions: [reviewAdoption()], connections: [reviewConnection()], adoptionsAvailable: true,
    connectionsLoading: false, currentArtifact: reviewArtifact(), artifactAvailable: true, now: reviewNow,
  });
  return renderToStaticMarkup(createElement(LandingUpdateReviewRecord, { review, state }));
}

describe('inspection receipt presentation', () => {
  it('shows exact server artifact and baseline observations with no deployment controls', () => {
    const html = render();
    expect(html).toContain('Review recorded');
    expect(html).toContain('prj_exact');
    expect(html).toContain('b'.repeat(64));
    expect(html).toContain('a'.repeat(64));
    expect(html).toContain('dpl_baseline');
    expect(html).toContain('dpl_original');
    expect(html).toContain('Addresses observed at review');
    expect(html).toContain('public access and application health were not checked');
    expect(html).toContain('Inspection only');
    expect(html).toContain('https://cooker.example.com');
    expect(html).not.toMatch(/<(?:button|input|select|form)\b/);
  });

  it('distinguishes an explicit empty alias observation from unavailable metadata', () => {
    const empty = render(landingUpdateReview({ baselineAliases: [] }));
    expect(empty).toContain('No aliases were reported for the baseline deployment at review time');
    expect(empty).not.toContain('href=');
    const missing = render(landingUpdateReview({ baselineAliases: undefined as unknown as string[] }));
    expect(missing).toContain('Some recorded address details are unavailable');
    expect(missing).not.toContain('No aliases were reported');
  });

  it('escapes provider text and omits credential-bearing or unsafe address links', () => {
    const html = render(landingUpdateReview({
      projectName: '<script>alert(1)</script>',
      baselineAliases: ['https://user:secret@cooker.example.com', 'javascript:alert(2)', 'https://cooker.example.com?token=secret'],
    }));
    expect(html).toContain('&lt;script&gt;alert(1)&lt;/script&gt;');
    expect(html).toContain('Needs inspection');
    expect(html).toContain('Some recorded address details are unavailable');
    expect(html).not.toContain('<script>');
    expect(html).not.toContain('href=');
    expect(html).not.toContain('user:secret');
    expect(html).not.toContain('token=secret');
  });

  it('keeps submission unavailable even when unexpected receipt data advertises it', () => {
    const html = render(landingUpdateReview({ submissionAvailable: true }));
    expect(html).toContain('Needs inspection');
    expect(html).toContain('This review cannot submit a deployment');
    expect(html).not.toMatch(/<(?:button|input|select|form)\b/);
  });
});
