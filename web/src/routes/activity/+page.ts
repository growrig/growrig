import { redirect } from '@sveltejs/kit';

// The Activity Log became the Log; keep old links and bookmarks working.
export const load = () => redirect(307, '/log');
