import { expect, type Page } from '@playwright/test';
import { mockApi } from './api';

export interface AnimInfo {
  name: string;
  playState: string;
  duration: number;
  iterations: number;
  /** Animated CSS properties across all keyframes (camelCase). */
  props: string[];
  target: string;
}

/**
 * Web Animations attached to elements inside #root, with timing and animated
 * properties. By default only *running* animations are returned: a finished
 * entrance held by `animation-fill-mode: both` is not motion.
 */
export async function runningAnimations(page: Page, onlyRunning = true): Promise<AnimInfo[]> {
  return page.evaluate((onlyRunning) => {
    const meta = new Set(['offset', 'easing', 'composite', 'computedOffset']);
    return document
      .getAnimations()
      .filter((a) => {
        const t = (a.effect as KeyframeEffect | null)?.target as Element | null;
        return !!t && !!t.closest('#root') && (!onlyRunning || a.playState === 'running');
      })
      .map((a) => {
        const eff = a.effect as KeyframeEffect;
        const timing = eff.getTiming();
        const frames = eff.getKeyframes() as Record<string, unknown>[];
        const props = Array.from(new Set(frames.flatMap((k) => Object.keys(k).filter((p) => !meta.has(p)))));
        const target = eff.target as Element;
        const cls = typeof target.className === 'string' ? target.className : (target.getAttribute('class') ?? '');
        return {
          name: (a as CSSAnimation).animationName ?? a.id ?? '',
          playState: a.playState,
          duration: typeof timing.duration === 'number' ? timing.duration : Number(timing.duration) || 0,
          iterations: timing.iterations ?? 1,
          props,
          target: `${target.tagName.toLowerCase()}.${cls}`.slice(0, 60),
        };
      });
  }, onlyRunning);
}

/** Elements inside #root that pin a compositor layer while idle. */
export async function willChangeViolations(page: Page): Promise<string[]> {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll('#root *'))
      .filter((el) => getComputedStyle(el).willChange !== 'auto')
      .map((el) => `${el.tagName.toLowerCase()}.${el.className?.toString?.().slice(0, 40)} will-change=${getComputedStyle(el).willChange}`),
  );
}

/** Force the scene-entrance class back on and read which keyframes the stars and edges would use. */
export async function entranceAnimationNames(page: Page): Promise<{ star: string; edge: string }> {
  return page.evaluate(() => {
    const canvas = document.querySelector('.canvas');
    canvas?.classList.add('is-entering');
    const star = getComputedStyle(document.querySelector('.star') as Element).animationName;
    const edge = getComputedStyle(document.querySelector('.constellation-path') as Element).animationName;
    canvas?.classList.remove('is-entering');
    return { star, edge };
  });
}

async function waitForConstellation(page: Page, stars: number): Promise<void> {
  await expect(page.locator('.star')).toHaveCount(stars);
  await expect.poll(() => page.locator('.react-flow__edges > *').count(), { timeout: 10_000 }).toBeGreaterThan(0);
}

/** Pipeline editor on the fixture pipeline, settled (stars measured, edges drawn). */
export async function gotoEditor(page: Page): Promise<void> {
  await mockApi(page);
  await page.goto('/pipelines/gates/edit');
  await waitForConstellation(page, 3);
}

/** Run view on the fixture run, settled. */
export async function gotoRun(page: Page): Promise<void> {
  await mockApi(page);
  await page.goto('/pipelines/gates/runs/r1');
  await waitForConstellation(page, 3);
  await expect(page.locator('.constellation.is-hot')).toHaveCount(1);
}
