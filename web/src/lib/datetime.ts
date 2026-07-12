import { preferences } from './preferences.svelte';

function parse(value: string | Date): Date {
	return value instanceof Date ? value : new Date(value);
}

function dtf(options: Intl.DateTimeFormatOptions = {}): Intl.DateTimeFormat {
	return new Intl.DateTimeFormat(preferences.locale, {
		timeZone: preferences.timezone,
		...options
	});
}

function format(value: string | Date, options: Intl.DateTimeFormatOptions): string {
	const d = parse(value);
	return isNaN(d.getTime()) ? '' : dtf(options).format(d);
}

function formatMs(ms: number, options: Intl.DateTimeFormatOptions): string {
	return dtf(options).format(new Date(ms));
}

/** Calendar date, e.g. "Jul 12, 2026" (en-US) or "12. 7. 2026" (cs-CZ). */
export function fmtDate(value: string | Date, options: Intl.DateTimeFormatOptions = { dateStyle: 'medium' }): string {
	return format(value, options);
}

/** Date and time, e.g. activity log entries. */
export function fmtDateTime(
	value: string | Date,
	options: Intl.DateTimeFormatOptions = { dateStyle: 'medium', timeStyle: 'short' }
): string {
	return format(value, options);
}

/** Time only — header clock, snapshot timestamps. */
export function fmtTime(
	value: string | Date,
	options: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit', second: '2-digit' }
): string {
	return format(value, options);
}

/** Live instance clock in the header. */
export function fmtClock(now: Date = new Date()): string {
	return dtf({ hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(now);
}

/** Chart x-axis: weekday + hour. */
export function fmtChartHour(ms: number): string {
	return formatMs(ms, { weekday: 'short', hour: '2-digit' });
}

/** Chart/tooltip: weekday + hour + minute. */
export function fmtChartHourMinute(value: string | number | Date): string {
	const d = typeof value === 'number' ? new Date(value) : parse(value);
	return isNaN(d.getTime()) ? '' : dtf({ weekday: 'short', hour: '2-digit', minute: '2-digit' }).format(d);
}

/** Camera snapshot strip. */
export function fmtSnapshotTime(value: string | Date): string {
	return format(value, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

/** Timeline midnight tick: weekday + day. */
export function fmtTimelineDay(ms: number): string {
	return formatMs(ms, { weekday: 'short', day: 'numeric' });
}

/** Timeline hourly tick label. */
export function fmtTimelineHour(ms: number): string {
	return formatMs(ms, { hour: '2-digit', minute: '2-digit' });
}
