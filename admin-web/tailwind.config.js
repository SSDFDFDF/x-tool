/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        void: '#030304',
        darkmatter: '#0F1115',
        bitcoin: '#F7931A',
        burnt: '#EA580C',
        gold: '#FFD600',
        muted: '#94A3B8',
        boundary: '#1E293B',
      },
      fontFamily: {
        heading: ['system-ui', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'sans-serif'],
        body: ['system-ui', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'sans-serif'],
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'monospace'],
      },
      boxShadow: {
        'glow-primary': '0 0 20px -5px rgba(234, 88, 12, 0.5)',
        'glow-primary-hover': '0 0 30px -5px rgba(247, 147, 26, 0.6)',
        'glow-gold': '0 0 20px rgba(255, 214, 0, 0.3)',
        'card-elevate': '0 0 50px -10px rgba(247, 147, 26, 0.1)',
        'card-hover': '0 0 30px -10px rgba(247, 147, 26, 0.2)',
        'input-focus': '0 10px 20px -10px rgba(247, 147, 26, 0.3)',
      },
      animation: {
        'float': 'float 8s ease-in-out infinite',
        'spin-slow': 'spin 10s linear infinite',
        'spin-slow-reverse': 'spin 15s linear infinite reverse',
      },
      keyframes: {
        float: {
          '0%, 100%': { transform: 'translateY(0px)' },
          '50%': { transform: 'translateY(-20px)' },
        }
      }
    },
  },
  plugins: [],
}
