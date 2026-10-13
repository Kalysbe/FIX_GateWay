/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      fontFamily: {
        sans: ["Onest", "Segoe UI", "sans-serif"],
        display: ["Onest", "Segoe UI", "sans-serif"],
        mono: ["\"JetBrains Mono\"", "ui-monospace", "monospace"],
      },
      colors: {
        ink: "#1a2332",
        panel: "#ffffff",
        surface: "#f4f7f8",
        line: "#e2e8f0",
        muted: "#5f6f84",
        sidebar: "#ffffff",
        accent: "#57b6c0",
        warn: "#c47a00",
        danger: "#d6455d",
      },
      boxShadow: {
        card: "0 1px 2px rgba(15, 23, 42, 0.05)",
      },
    },
  },
  plugins: [],
};
