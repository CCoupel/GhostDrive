/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        brand: {
          DEFAULT: '#0078D4',
          hover: '#106EBE',
          light: '#C7E0F4',
        },
        surface: {
          DEFAULT: '#FFFFFF',
          secondary: '#F8F9FA',
          border: '#E1DFDD',
        },
        status: {
          idle: '#107C10',
          syncing: '#0078D4',
          paused: '#797775',
          error: '#D13438',
        },
      },
      fontFamily: {
        sans: ['"Segoe UI"', 'system-ui', 'sans-serif'],
      },
    },
  },
  plugins: [],
}
