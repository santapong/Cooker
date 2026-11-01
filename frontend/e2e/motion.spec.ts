import { expect, test } from '@playwright/test';
import { entranceAnimationNames, gotoEditor, gotoRun, runningAnimations } from './fixtures/motion';

/** Ambient loops are allowed to move; everything else must be opacity-only under reduced motion. */
const AMBIENT = new Set(['drift1', 'drift2', 'twinkle']);

test.describe('spec §6.1 — reduced motion substitutes, never deletes', () => {
  test.use({ reducedMotion: 'reduce' });
  test.beforeEach(async ({ page }) => {
    // Belt and braces: the context option should do this, but the media query
    // must be in force before the app's matchMedia hook mounts.
    await page.emulateMedia({ reducedMotion: 'reduce' });
  });

  test('the porthole still opens (by opacity) and nothing animates transform', async ({ page }) => {
    await gotoEditor(page);
    expect(await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches), 'reduced-motion emulation is in force').toBe(true);
    await expect.poll(() => page.locator('.porthole').evaluate((el) => getComputedStyle(el).opacity), { timeout: 2_000 }).toBe('1');
    const anims = await runningAnimations(page);
    for (const a of anims) {
      expect(a.props, `${a.name} on ${a.target} must not animate transform under reduced motion`).not.toContain('transform');
      expect(AMBIENT.has(a.name), `ambient ${a.name} must be static under reduced motion`).toBe(false);
    }
  });

  test('scene entrances resolve to opacity fades', async ({ page }) => {
    await gotoEditor(page);
    expect(await entranceAnimationNames(page)).toEqual({ star: 'fade-in', edge: 'fade-in' });
  });

  test('run view: no comet, the hot edge keeps its static stroke, the halo pulse is opacity-only', async ({ page }) => {
    await gotoRun(page);
    await expect(page.locator('.comet')).toHaveCount(0);
    await expect(page.locator('.constellation.is-hot')).toHaveCount(1);
    const halo = (await runningAnimations(page)).find((a) => a.target.includes('halo'));
    expect(halo?.name).toBe('halo-pulse-static');
    expect(halo?.props).toEqual(['opacity']);
  });
});

test.describe('spec §6.2 — flash ceiling (SC 2.3.1) and the motion budget', () => {
  test('twinkle runs ≥ 7 s and no infinite opacity loop cycles faster than 3 Hz', async ({ page }) => {
    await gotoEditor(page);
    const twinkles = await page.$$eval('.starfield .tw', (els) =>
      els.map((el) => {
        const a = el.getAnimations()[0];
        const d = a ? (a.effect as KeyframeEffect).getTiming().duration : 0;
        return typeof d === 'number' ? d : Number(d) || 0;
      }),
    );
    expect(twinkles.length).toBeGreaterThan(0);
    for (const d of twinkles) expect(d).toBeGreaterThanOrEqual(7_000);

    const anims = await runningAnimations(page);
    for (const a of anims.filter((x) => x.iterations === Infinity && x.props.includes('opacity'))) {
      expect(a.duration, `${a.name} on ${a.target} cycles opacity too fast`).toBeGreaterThanOrEqual(1000 / 3);
    }
    const names = new Set(anims.map((a) => a.name));
    expect(names.has('drift1') && names.has('drift2') && names.has('twinkle'), 'ambient starfield motion is present under full motion').toBe(true);
    // Only the ambient drift may move on transform while idle.
    for (const a of anims.filter((x) => x.props.includes('transform'))) expect(AMBIENT.has(a.name), `${a.name} moves on transform at idle`).toBe(true);
  });

  test('scene entrances use their full-motion keyframes', async ({ page }) => {
    await gotoEditor(page);
    expect(await entranceAnimationNames(page)).toEqual({ star: 'star-in', edge: 'draw' });
  });

  test('run view: the comet rides the hot edge and the halo pulses no faster than once a second', async ({ page }) => {
    await gotoRun(page);
    await expect(page.locator('.constellation.is-hot .comet animateMotion')).toHaveCount(1);
    const halo = (await runningAnimations(page)).find((a) => a.name === 'halo-pulse');
    expect(halo, 'running star halo pulse').toBeTruthy();
    expect(halo!.duration).toBeGreaterThanOrEqual(1_000);
    expect(halo!.iterations).toBe(Infinity);
  });
});
