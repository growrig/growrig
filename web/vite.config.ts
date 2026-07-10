import process from 'node:process';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	server: {
		// Honor a PORT assigned by the environment (e.g. the preview harness).
		port: process.env.PORT ? Number(process.env.PORT) : 5173
	},
	plugins: [
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},

			// Static SPA build — embedded into and served by the Grow Core
			// binary (see growcore/internal/webui). index.html is the SPA
			// fallback for client-side routes.
			adapter: adapter({ fallback: 'index.html' })
		})
	]
});
