module.exports = {
  presets: [require("@grewelltech/console/tailwind-preset")],
  content: [
    "./index.html",
    "./src/**/*.{ts,tsx}",
    "./node_modules/@grewelltech/console/src/**/*.tsx",
    "../design-system/src/**/*.tsx",
  ],
  theme: {
    extend: {
      // The preset hardcodes the Plex stacks in fontFamily, so font-sans /
      // font-mono utilities would ignore the --gtc-font-* tokens. Bind them
      // to the variables (whose defaults ARE the Plex stacks) so data-font
      // switches every font-sans/font-mono element, not just body text.
      // Upstream preset candidate: the preset should do this itself.
      fontFamily: {
        sans: "var(--gtc-font-sans)",
        mono: "var(--gtc-font-mono)",
      },
      colors: {
        // donezo project-color ramp — muted categorical hues that sit on
        // GTech Console's ink-navy without competing with the cyan accent.
        pj: {
          blue: "var(--dz-pj-blue)",
          green: "var(--dz-pj-green)",
          tan: "var(--dz-pj-tan)",
          violet: "var(--dz-pj-violet)",
          rose: "var(--dz-pj-rose)",
          orange: "var(--dz-pj-orange)",
          steel: "var(--dz-pj-steel)",
        },
      },
    },
  },
};
