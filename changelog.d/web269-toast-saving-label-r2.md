### Fixed

- **A save button now says "Saving…" while its request runs, and a form the browser refuses to submit withdraws the previous "Saved."** The `data-saving-label` the templates declared was never read, so the button kept its idle text; and native validation sent no request, leaving the earlier success standing beside the validation bubble.
