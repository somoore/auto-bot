/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        void: "#0B0D14",
        sky: "#13161F",
        atmos: "#1B1F2B",
        edge: "#252A39",
        star: "#F0E9D6",
        twilight: "#8B92A6",
        farstar: "#525A6E",
        aurora: "#3CDFB1",
        solar: "#FF8C42",
        magnetar: "#FF3D7F",
        comet: "#6BB4FF",
      },
    },
  },
  plugins: [],
}
