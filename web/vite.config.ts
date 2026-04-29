import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';

function manualChunksPlugin(): Plugin {
  return {
    name: 'manual-chunks',
    config() {
      return {
        build: {
          rollupOptions: {
            output: {
              manualChunks(id) {
                if (id.includes('node_modules/recharts')) return 'recharts';
                if (id.includes('node_modules/antd') || id.includes('node_modules/@ant-design')) return 'antd';
                if (id.includes('node_modules/react') || id.includes('node_modules/react-dom') || id.includes('node_modules/react-router')) return 'vendor';
              },
            },
          },
        },
      };
    },
  };
}

export default defineConfig({
  plugins: [react(), manualChunksPlugin()],
  server: {
    port: 34892,
    proxy: {
      '/api': {
        target: 'http://localhost:34891',
        changeOrigin: true,
      },
      '/v1': {
        target: 'http://localhost:34891',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: '../cmd/JoyCodeProxy/static',
    emptyOutDir: true,
  },
});
