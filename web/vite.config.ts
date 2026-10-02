import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'
import { mockApiPlugin } from './mock-api.ts'
export default defineConfig(({mode})=>({plugins:[react(),tailwindcss(),...(mode==='mock'?[mockApiPlugin()]:[])],resolve:{alias:{'@':fileURLToPath(new URL('./src',import.meta.url))}},test:{environment:'jsdom',setupFiles:['./src/test-setup.ts'],exclude:['node_modules/**']}}))
