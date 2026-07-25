module.exports = {
  presets: [require("@grewelltech/aether/tailwind-preset")],
  content: [
    "./index.html",
    "./src/**/*.{ts,tsx}",
    "./node_modules/@grewelltech/aether/src/**/*.tsx",
    "../design-system/src/**/*.tsx",
  ],
  theme: {
    extend: {
      colors: {
        // donezo project-color ramp — muted categorical hues that sit on
        // Aether's ink-navy without competing with the cyan accent.
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
