const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * Milliseconds from `now` to the next midnight in `timeZone` (the runner's own
 * zone when absent): the moment the server's "today" for a request carrying
 * that zone moves to the next date.
 */
export function msUntilMidnight(now: Date, timeZone?: string): number {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone,
    hourCycle: 'h23',
    hour: 'numeric',
    minute: 'numeric',
    second: 'numeric',
  }).formatToParts(now);
  const part = (type: Intl.DateTimeFormatPartTypes) =>
    Number(parts.find((candidate) => candidate.type === type)?.value ?? 0);
  const intoDay =
    ((part('hour') * 60 + part('minute')) * 60 + part('second')) * 1000 + now.getMilliseconds();
  return DAY_MS - intoDay;
}
