import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig({
  plugins: [react(), {
    name: 'local-mount-api',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const address = req.socket.remoteAddress;
        if (req.url?.startsWith('/api') && !['127.0.0.1', '::1', '::ffff:127.0.0.1'].includes(address || '')) {
          res.statusCode = 403;
          res.setHeader('Content-Type', 'application/json; charset=utf-8');
          res.end(JSON.stringify({ error: { code: 'local_only', message: '挂载服务目前仅允许本机访问' } }));
          return;
        }
        next();
      });
    },
  }],
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:8081', changeOrigin: true },
    },
  },
});
