import { expect, type Locator, type Page } from '@playwright/test';

/**
 * WCAG 2.1 AA minimum contrast for body-sized text. The primary action renders
 * 0.88rem/700 white text, which is below the large-text threshold, so 4.5:1 is
 * the bar that applies to it.
 */
export const WCAG_AA_TEXT_CONTRAST = 4.5;

/**
 * WCAG 2.1 AA minimum contrast for a non-text graphical object that carries
 * meaning (1.4.11). The cycle ring's track is one: it is the remainder of the
 * cycle the coloured phase segments are drawn on.
 */
export const WCAG_AA_GRAPHIC_CONTRAST = 3;

interface Rgba {
  r: number;
  g: number;
  b: number;
  a: number;
}

interface PaintedText {
  measurable: boolean;
  reason: string;
  color: string;
  backgroundColor: string;
  backgroundImage: string;
  /** Ancestor background colors, nearest first, used to flatten translucency. */
  backdrop: string[];
}

export interface ContrastStop {
  /** The stop as the browser reported it, before flattening. */
  source: string;
  /** The stop flattened over its backdrop, as `rgb(r, g, b)`. */
  painted: string;
  ratio: number;
}

export interface ContrastMeasurement {
  label: string;
  color: string;
  backgroundColor: string;
  backgroundImage: string;
  stops: ContrastStop[];
  worstRatio: number;
}

function parseCssColor(value: string): Rgba | null {
  const input = value.trim().toLowerCase();
  if (input === 'transparent') {
    return { r: 0, g: 0, b: 0, a: 0 };
  }

  const functional = /^rgba?\(([^)]*)\)$/.exec(input);
  if (!functional) {
    return null;
  }

  const parts = functional[1].split(/[\s,/]+/).filter((part) => part.length > 0);
  if (parts.length < 3 || parts.length > 4) {
    return null;
  }

  const channel = (raw: string, scale: number): number | null => {
    const numeric = raw.endsWith('%')
      ? (Number.parseFloat(raw.slice(0, -1)) / 100) * scale
      : Number.parseFloat(raw);
    return Number.isFinite(numeric) ? numeric : null;
  };

  const r = channel(parts[0], 255);
  const g = channel(parts[1], 255);
  const b = channel(parts[2], 255);
  const a = parts.length === 4 ? channel(parts[3], 1) : 1;
  if (r === null || g === null || b === null || a === null) {
    return null;
  }

  return { r, g, b, a };
}

function formatRgb(color: Rgba): string {
  const clamp = (value: number): number => Math.min(255, Math.max(0, Math.round(value)));
  return `rgb(${clamp(color.r)}, ${clamp(color.g)}, ${clamp(color.b)})`;
}

function flattenOver(top: Rgba, bottom: Rgba): Rgba {
  const alpha = top.a;
  return {
    r: top.r * alpha + bottom.r * (1 - alpha),
    g: top.g * alpha + bottom.g * (1 - alpha),
    b: top.b * alpha + bottom.b * (1 - alpha),
    a: 1,
  };
}

function relativeLuminance(color: Rgba): number {
  const linear = (channel: number): number => {
    const normalized = channel / 255;
    return normalized <= 0.03928
      ? normalized / 12.92
      : Math.pow((normalized + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * linear(color.r) + 0.7152 * linear(color.g) + 0.0722 * linear(color.b);
}

export function contrastRatio(first: Rgba, second: Rgba): number {
  const a = relativeLuminance(first);
  const b = relativeLuminance(second);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

/**
 * Every colour a background layer can paint. A solid fill contributes one stop;
 * a gradient contributes each of its stops, because text sits on all of them and
 * the weakest one is what a reader gets. Automated a11y engines report a
 * `background-image` as *incomplete* rather than as a violation, so resolving the
 * stops here is the whole point of this helper.
 */
function backgroundStops(backgroundColor: string, backgroundImage: string): string[] {
  const stops: string[] = [];

  if (backgroundImage !== 'none' && backgroundImage.trim().length > 0) {
    const matches = backgroundImage.match(/rgba?\([^)]*\)/g);
    if (matches) {
      stops.push(...matches);
    }
  }

  const solid = parseCssColor(backgroundColor);
  if (solid !== null && solid.a > 0) {
    stops.push(backgroundColor);
  }

  return stops;
}

async function readPaintedText(locator: Locator): Promise<PaintedText> {
  return locator.evaluate((node: Element): PaintedText => {
    const style = window.getComputedStyle(node);
    const rect = node.getBoundingClientRect();

    const backdrop: string[] = [];
    for (let parent = node.parentElement; parent !== null; parent = parent.parentElement) {
      backdrop.push(window.getComputedStyle(parent).backgroundColor);
    }
    backdrop.push(window.getComputedStyle(document.documentElement).backgroundColor);

    const painted: PaintedText = {
      measurable: true,
      reason: '',
      color: style.color,
      backgroundColor: style.backgroundColor,
      backgroundImage: style.backgroundImage,
      backdrop,
    };

    if (style.display === 'none' || style.visibility === 'hidden' || rect.width === 0) {
      painted.measurable = false;
      painted.reason = 'element is not rendered';
    } else if ((node as HTMLButtonElement).disabled === true) {
      // WCAG 1.4.3 exempts text in an inactive user interface component.
      painted.measurable = false;
      painted.reason = 'element is disabled';
    }

    return painted;
  });
}

/**
 * Resolves the colours the element actually paints and the contrast of its text
 * against each of them. An unresolvable layer throws rather than being treated as
 * a pass — a background this helper cannot flatten is unknown, not compliant.
 */
export async function measureTextContrast(
  locator: Locator,
  label: string
): Promise<ContrastMeasurement | null> {
  const painted = await readPaintedText(locator);
  if (!painted.measurable) {
    return null;
  }

  const text = parseCssColor(painted.color);
  if (text === null) {
    throw new Error(`${label}: unresolvable text color ${painted.color}`);
  }

  const backdropSource = painted.backdrop
    .map((value) => parseCssColor(value))
    .find((value): value is Rgba => value !== null && value.a === 1);
  // No opaque ancestor anywhere is only reachable on a page with a transparent
  // root; the canvas is white then, which is also the harsher assumption for the
  // white text this component paints.
  const backdrop: Rgba = backdropSource ?? { r: 255, g: 255, b: 255, a: 1 };

  const sources = backgroundStops(painted.backgroundColor, painted.backgroundImage);
  if (sources.length === 0) {
    throw new Error(
      `${label}: paints no background of its own ` +
        `(background-color: ${painted.backgroundColor}, background-image: ${painted.backgroundImage})`
    );
  }

  const stops: ContrastStop[] = sources.map((source) => {
    const parsed = parseCssColor(source);
    if (parsed === null) {
      throw new Error(`${label}: unresolvable background stop ${source}`);
    }
    const flattened = flattenOver(parsed, backdrop);
    const flatText = flattenOver(text, flattened);
    return {
      source,
      painted: formatRgb(flattened),
      ratio: contrastRatio(flatText, flattened),
    };
  });

  return {
    label,
    color: painted.color,
    backgroundColor: painted.backgroundColor,
    backgroundImage: painted.backgroundImage,
    stops,
    worstRatio: Math.min(...stops.map((stop) => stop.ratio)),
  };
}

interface PaintedSurface {
  measurable: boolean;
  reason: string;
  backgroundColor: string;
  backgroundImage: string;
  backdrop: string[];
}

/**
 * The colours a surface (or one of its decorative pseudo-element layers) paints,
 * plus the ancestor colours needed to flatten translucency. `pseudo` reads a
 * `::before`/`::after` layer, whose own backdrop starts at the element's
 * background — a decoration is painted on top of it.
 */
async function readPaintedSurface(locator: Locator, pseudo?: string): Promise<PaintedSurface> {
  return locator.evaluate((node: Element, selector: string | null): PaintedSurface => {
    const style = window.getComputedStyle(node, selector);
    const rect = node.getBoundingClientRect();

    const backdrop: string[] = [];
    if (selector !== null) {
      backdrop.push(window.getComputedStyle(node).backgroundColor);
    }
    for (let parent = node.parentElement; parent !== null; parent = parent.parentElement) {
      backdrop.push(window.getComputedStyle(parent).backgroundColor);
    }
    backdrop.push(window.getComputedStyle(document.documentElement).backgroundColor);

    const painted: PaintedSurface = {
      measurable: true,
      reason: '',
      backgroundColor: style.backgroundColor,
      backgroundImage: style.backgroundImage,
      backdrop,
    };

    const hostStyle = window.getComputedStyle(node);
    if (hostStyle.display === 'none' || hostStyle.visibility === 'hidden' || rect.width === 0) {
      painted.measurable = false;
      painted.reason = 'element is not rendered';
    }

    return painted;
  }, pseudo ?? null);
}

function opaqueBackdrop(values: string[]): Rgba {
  const found = values
    .map((value) => parseCssColor(value))
    .find((value): value is Rgba => value !== null && value.a === 1);
  // No opaque ancestor anywhere is only reachable on a page with a transparent
  // root; the canvas is white then.
  return found ?? { r: 255, g: 255, b: 255, a: 1 };
}

/** Every colour the layer declares, parsed but not yet composited. */
function parsedStops(painted: PaintedSurface, label: string): { source: string; color: Rgba }[] {
  return backgroundStops(painted.backgroundColor, painted.backgroundImage).map((source) => {
    const parsed = parseCssColor(source);
    if (parsed === null) {
      throw new Error(`${label}: unresolvable background stop ${source}`);
    }
    return { source, color: parsed };
  });
}

/** The same stops, flattened over the nearest opaque colour behind the layer. */
function flattenedStops(painted: PaintedSurface, label: string): { source: string; color: Rgba }[] {
  const backdrop = opaqueBackdrop(painted.backdrop);
  return parsedStops(painted, label).map((stop) => ({
    source: stop.source,
    color: flattenOver(stop.color, backdrop),
  }));
}

/**
 * Contrast of a graphic's paint against every colour the surface beneath it can
 * paint. `worstRatio` is the weakest of them, because a reader gets the weakest
 * one. The paint is an SVG `stroke` or `fill`, or the `background-color` of a
 * plain element — a data mark does not have to be drawn in SVG to be a
 * graphical object, and the cycle ribbon's day cells are div backgrounds.
 */
export async function measureGraphicContrast(
  graphic: Locator,
  surface: Locator,
  label: string,
  property: 'stroke' | 'fill' | 'background-color' = 'stroke'
): Promise<ContrastMeasurement> {
  const paint = await graphic.evaluate(
    (node: Element, name: string) => window.getComputedStyle(node).getPropertyValue(name).trim(),
    property
  );
  const graphicColor = parseCssColor(paint);
  if (graphicColor === null) {
    throw new Error(`${label}: unresolvable ${property} ${paint}`);
  }

  const painted = await readPaintedSurface(surface);
  if (!painted.measurable) {
    throw new Error(`${label}: the surface is unmeasurable (${painted.reason})`);
  }

  const surfaceStops = flattenedStops(painted, `${label} surface`);
  if (surfaceStops.length === 0) {
    throw new Error(
      `${label}: the surface paints no background of its own ` +
        `(background-color: ${painted.backgroundColor}, background-image: ${painted.backgroundImage})`
    );
  }

  const stops: ContrastStop[] = surfaceStops.map((stop) => ({
    source: stop.source,
    painted: formatRgb(stop.color),
    ratio: contrastRatio(flattenOver(graphicColor, stop.color), stop.color),
  }));

  return {
    label,
    color: paint,
    backgroundColor: painted.backgroundColor,
    backgroundImage: painted.backgroundImage,
    stops,
    worstRatio: Math.min(...stops.map((stop) => stop.ratio)),
  };
}

export function describeContrast(measurement: ContrastMeasurement): string {
  const stops = measurement.stops
    .map((stop) => `${stop.painted} -> ${stop.ratio.toFixed(2)}:1`)
    .join('; ');
  return (
    `${measurement.label}: ${measurement.color} on ` +
    `[background-color: ${measurement.backgroundColor}, background-image: ${measurement.backgroundImage}] ` +
    `stops { ${stops} }`
  );
}

/**
 * Asserts AA text contrast for every rendered, enabled match of `selector`, and
 * fails when nothing was measured — a selector that matches nothing must not read
 * as a pass.
 */
export async function expectTextContrastAA(
  page: Page,
  selector: string,
  label: string,
  minimum: number = WCAG_AA_TEXT_CONTRAST
): Promise<ContrastMeasurement[]> {
  const targets = page.locator(selector);
  const count = await targets.count();
  expect(count, `${label}: expected at least one element matching ${selector}`).toBeGreaterThan(0);

  const measured: ContrastMeasurement[] = [];
  for (let index = 0; index < count; index += 1) {
    const measurement = await measureTextContrast(targets.nth(index), `${label} #${index}`);
    if (measurement === null) {
      continue;
    }
    measured.push(measurement);
    expect(measurement.worstRatio, describeContrast(measurement)).toBeGreaterThanOrEqual(minimum);
  }

  expect(
    measured.length,
    `${label}: every element matching ${selector} was skipped, so nothing was checked`
  ).toBeGreaterThan(0);

  return measured;
}

/** Switches the client-side theme and waits for the concrete `data-theme` signal. */
export async function applyTheme(page: Page, theme: 'light' | 'dark'): Promise<void> {
  await page.evaluate((value) => {
    window.localStorage.setItem('ovumcy_theme', value);
  }, theme);
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
}
