none

E2E only: the browser-error fixture tolerates, on the webkit project only, the refused user-agent
stylesheet that WebKit 27.2 (Playwright 1.64) reports for every page with a select under
style-src 'self'. The Content Security Policy and the pages are unchanged.
