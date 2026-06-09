// Tailwind scans the Go HTML templates and handler markup listed under
// `content` for utility classes (these globs drive purge). The stylesheet is
// built by scripts/build-css.mjs into web/static/css/tailwind.css.
/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    './internal/templates/**/*.html',
    './internal/api/**/*.go',
    './cmd/**/*.go'
  ],
  theme: {
    extend: {
      boxShadow: {
        glow: '0 0 0 1px rgba(56, 189, 248, 0.35), 0 12px 24px -12px rgba(6, 182, 212, 0.55)'
      }
    }
  },
  plugins: []
};
