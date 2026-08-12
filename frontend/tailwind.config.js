/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      fontFamily: {
        display: ["\"IBM Plex Sans\"", "Segoe UI", "sans-serif"],
        mono: ["\"IBM Plex Mono\"", "ui-monospace", "monospace"],
      },
      colors: {
        ink: "#1a2332",
        panel: "#ffffff",
        surface: "#f4f7f8",
        accent: "#57b6c0",
        warn: "#c47a00",
        danger: "#d6455d",
      },
    },
  },
  plugins: [],
};
