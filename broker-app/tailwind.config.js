/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      fontFamily: {
        display: ['"Space Grotesk"', "Segoe UI", "sans-serif"],
        sans: ['"DM Sans"', "Segoe UI", "sans-serif"],
        mono: ['"IBM Plex Mono"', "ui-monospace", "monospace"],
      },
      colors: {
        ink: "#10151c",
        panel: "#171e28",
        accent: "#5eead4",
        warn: "#fbbf24",
        danger: "#f87171",
        buy: "#34d399",
        sell: "#fb7185",
      },
    },
  },
  plugins: [],
};
