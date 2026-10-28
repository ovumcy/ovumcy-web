// WEB-245: the export card's range summary is re-rendered client-side after
// the summary fetch. It used a locale table that knew only ru/es/fr, so a de
// or it page got English month names, and it quoted the selected range — whose
// end defaults to today — instead of the range of the entries it counts.

import test from "node:test";
import assert from "node:assert/strict";
import { readSettingsExportBundle, loadDOMWithScript } from "./_helpers.mjs";

const SETTINGS_EXPORT_BUNDLE = readSettingsExportBundle();

function exportPage(lang, rangeTemplate) {
  return `<!doctype html><html lang="${lang}"><head></head><body>
  <section data-export-section
    data-export-min="2026-07-20"
    data-export-max="2026-10-05"
    data-export-summary-total-template="%d"
    data-export-summary-range-template="${rangeTemplate}">
    <input id="export-from" type="text" value="2026-07-20">
    <input id="export-to" type="text" value="2026-10-05">
    <button type="button" data-export-action data-export-endpoint="/api/v1/exports/csv">CSV</button>
    <p data-export-summary-total></p>
    <p data-export-summary-range></p>
  </section>
</body></html>`;
}

const SUMMARY_PAYLOAD = { total_entries: 3, has_data: true, date_from: "2026-07-20", date_to: "2026-08-21" };

for (const { lang, rangeTemplate } of [
  { lang: "de", rangeTemplate: "Zeitraum: %s bis %s" },
  { lang: "it", rangeTemplate: "Intervallo date: %s a %s" },
]) {
  test(`export summary on a ${lang} page names ${lang} months and ends at the last entry`, async () => {
    const dom = await loadDOMWithScript(SETTINGS_EXPORT_BUNDLE, {
      html: exportPage(lang, rangeTemplate),
      beforeRun(window) {
        window.fetch = async () => ({ ok: true, json: async () => SUMMARY_PAYLOAD });
      },
    });
    try {
      const summaryNode = dom.window.document.querySelector("[data-export-summary-range]");
      for (let attempt = 0; attempt < 50 && !summaryNode.textContent; attempt++) {
        await new Promise((resolve) => setTimeout(resolve, 20));
      }

      const formatter = new Intl.DateTimeFormat(lang, { year: "numeric", month: "short", day: "numeric" });
      const expected = rangeTemplate
        .replace("%s", formatter.format(new Date(2026, 6, 20)))
        .replace("%s", formatter.format(new Date(2026, 7, 21)));
      assert.equal(summaryNode.textContent, expected);
    } finally {
      dom.window.close();
    }
  });
}
