/**
 * ISO-date arithmetic on the runner's own clock, shared by the e2e helpers and
 * the specs.
 *
 * This module imports nothing — not Playwright, not another helper — and that is
 * the point: `stats-helpers` already imports `auth-helpers`, so hosting these two
 * functions there would force `auth-helpers` to import back and close a cycle
 * between the two biggest helper modules. A leaf both sides can depend on has no
 * such edge. Keep it dependency-free.
 */

/** Shifts an ISO date by whole days in local time (a negative count goes back). */
export function shiftISODate(iso: string, days: number): string {
  const [year, month, day] = iso.split('-').map((part) => Number(part));
  const shifted = new Date(year, month - 1, day);
  shifted.setDate(shifted.getDate() + days);
  const yyyy = shifted.getFullYear();
  const mm = String(shifted.getMonth() + 1).padStart(2, '0');
  const dd = String(shifted.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

/**
 * Today by the runner's own clock, as an ISO date. Distinct from
 * `todayISOFromDashboard`, which reads the date the *server* rendered: use this
 * one to compute the dates a scenario seeds, and that one when the assertion is
 * about the day the app itself thinks it is.
 */
export function isoToday(): string {
  const date = new Date();
  date.setHours(0, 0, 0, 0);
  const yyyy = date.getFullYear();
  const mm = String(date.getMonth() + 1).padStart(2, '0');
  const dd = String(date.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}
